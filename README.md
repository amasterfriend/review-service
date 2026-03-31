# review-service

一个基于 `go-kratos` 的评价服务，核心职责是：

- 写路径：处理 C/B/O 端评价相关请求并落 MySQL。
- 读路径：按商家分页查评价时，优先走 Redis 缓存，未命中查 Elasticsearch。
- 服务治理：通过 Consul 做注册发现。

## 1. 项目结构

```text
review-service
├── cmd/review-service      # 启动入口、wire 依赖注入
├── api/review/v1           # protobuf + HTTP/GRPC 路由定义
├── internal/server         # HTTP/GRPC server、中间件、Consul 注册
├── internal/service        # 接口层（DTO 转换）
├── internal/biz            # 业务规则层（防重复、越权、状态流转）
├── internal/data           # 数据访问层（MySQL/Redis/ES）
├── configs                 # 配置（服务、DB、Redis、ES、Consul）
├── review.sql              # 表结构
└── docker-compose.yaml     # 本地依赖中间件
```

## 2. 功能接口

`api/review/v1/review.proto` 定义了以下接口：

- C 端：创建评价、查看评价详情、按商家查评价列表
- B 端：回复评价、申诉评价
- O 端：审核评价、审核申诉

说明：

- `ListReviewByUserID` 在 proto 中有定义，但当前 `internal/service/review.go` 中未实现业务逻辑（会走未实现默认返回）。

## 3. 中间件与存储职责

### MySQL（主存储，强一致真相源）

- 表：`review_info`、`review_reply_info`、`review_appeal_info`（见 `review.sql`）。
- 职责：
  - 评价/回复/申诉数据持久化。
  - 审核状态、运营操作信息持久化。
  - `SaveReply` 和 `AuditAppeal` 使用事务保证多表更新一致性。

### Redis（查询缓存）

- 仅用于 `ListReviewByStoreID` 查询缓存。
- key 规则：`review:{storeID}:{offset}:{limit}`
- value：ES 搜索结果中的 `hits` JSON。
- TTL：10 秒。
- 配合 `singleflight` 抑制同 key 并发击穿。

### Elasticsearch（检索存储）

- 用于商家维度分页检索（`store_id` 过滤）。
- `review-service` 只读 ES，不直接写 ES。
- 查询时从index `review` 读（`internal/data/review.go`）。

### Kafka（变更消息总线）

- `review-service` 本身不生产/消费 Kafka。
- Kafka 在整个系统中的作用是承接 MySQL Binlog 变更事件，供下游同步 ES。

### Canal（Binlog CDC）

- 从 MySQL Binlog 订阅变更，写入 Kafka topic（本地配置为 `example`）。
- 依赖 MySQL 开启 Binlog（`my.cnf` 已配置 `log-bin` 和 `binlog-format=ROW`）。

### Consul（服务注册发现）

- `review-service` 启动后注册到 Consul（`configs/registry.yaml`）。

## 4. 数据流转（重点）

### 4.1 写路径：CreateReview/Reply/Audit

```text
Client -> review-service(HTTP/GRPC)
       -> service -> biz(业务校验/ID生成) -> data
       -> MySQL(review_* 表)
```

这是业务提交链路，最终以 MySQL 为准。

### 4.2 检索路径：ListReviewByStoreID

```text
Client -> review-service
       -> Redis(key=review:store:offset:limit)
          -> hit: 直接返回
          -> miss: singleflight 合并请求 -> ES(index=review)
                    -> 回填 Redis(10s) -> 返回
```

### 4.3 MySQL 到 ES 的异步同步链路

```text
review-service 写 MySQL
  -> MySQL Binlog
  -> Canal 订阅 Binlog
  -> Kafka(topic=example) 存变更事件
  -> review-job 消费 Kafka
  -> 写入/更新 Elasticsearch(index=review, _id=review_id)
  -> review-service 查询 ES
```

关键点：

- 这是一条**异步最终一致**链路，不是强一致。
- 刚写入 MySQL 后，ES 可能短暂查不到（同步延迟 + 缓存 TTL）。

## 5. Kafka 和 ES 里“到底存了什么”

### Kafka（存的是变更事件，不是业务查询模型）

Canal 推到 Kafka 的消息是 CDC 事件，典型结构类似：

```json
{
  "type": "INSERT",
  "database": "review_db",
  "table": "review_info",
  "is_ddl": false,
  "data": [
    {
      "review_id": "1234567890",
      "store_id": "20001",
      "user_id": "30001",
      "content": "很好",
      "status": "10"
    }
  ]
}
```

`review-job` 会根据 `type` 做：

- `INSERT` -> `Index`
- 其他类型（如 `UPDATE`）-> `Update`

### Elasticsearch（存的是可检索文档）

- 索引：`review`
- 文档 `_id`：`review_id`
- 文档内容：来自 Canal 事件 `data[i]` 的字段集合（近似一行 `review_info`）

因此 ES 里本质是“面向检索的一份副本数据”。

## 6. 快速启动（本地）

### 6.1 启依赖

```bash
cd /home/wslubuntu/learn_go/review-repo/review-service
docker compose up -d
```

启动后可用端口：

- MySQL `3306`
- Redis `6379`
- Kafka `9092`
- Kafka UI `8070`
- ES `9200`
- Kibana `5601`
- Consul `8500`
- Canal `11111`

### 6.2 初始化数据库

```bash
mysql -h127.0.0.1 -uroot -p123456 review_db < review.sql
```

### 6.3 启服务

```bash
go run ./cmd/review-service -conf ./configs
```

HTTP 默认：`0.0.0.0:8000`  
gRPC 默认：`0.0.0.0:9000`

## 7. 常见排查

- 创建评价成功但按商家查不到：
  - 先查 MySQL 是否有数据。
  - 再看 Kafka 是否有 CDC 事件。
  - 再看 `review-job` 是否在消费并写 ES。
  - 最后清理 Redis 缓存 key 再重试。
- ES 有数据但接口没返回：
  - 检查 ES 字段类型和反序列化格式是否匹配（代码里按字符串数字处理）。
- 审核/申诉状态不符合预期：
  - 对照 `review.sql` 状态定义：评价 `10/20/30/40`，申诉 `10/20/30`。

