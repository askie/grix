# Grix infra Kubernetes examples

`k8s/infra/base` 提供基础设施示例清单：`postgres`、`redis`、`nats`，以及可选的 `livekit` / `coturn`。

## 目录

- `base/`：基础 Kustomize 清单
- `secret.example.yaml`：基础设施 Secret 示例（复制后本地填写，勿提交）
- `egress-secret.example.yaml`：LiveKit Egress 出口示例

本公开仓库不保留可执行生产发布脚本；生产发版入口在私有运维仓库维护。

## 使用方式

1. 复制示例 Secret 到本地忽略路径并替换占位符：

```bash
cp k8s/infra/secret.example.yaml /tmp/grix-infra-secret.yaml
# 按需编辑 /tmp/grix-infra-secret.yaml
```

2. 按你的集群修改存储类、资源请求和镜像地址（`k8s/infra/base/**`）。

3. 用 `kustomize build k8s/infra/base` 渲染后自行 `kubectl apply`。

生产 overlay、区域差异、真实镜像仓库坐标和统一发布入口不在本公开仓库。

## 关键约束

- `postgres` 示例使用 `pgvector/pgvector:pg15`，初始化脚本会启用 `vector` 扩展
- `postgres` / `redis` / `nats` 使用 `StatefulSet + PVC`
- 业务侧服务名示例为 `aibot-postgres`、`aibot-redis`、`aibot-nats`；自建环境可改名，但要同步改应用配置
- 应用层与基础设施层的数据库/缓存密码必须一致
- LiveKit / coturn 示例默认偏单机验证；公网 IP、防火墙与 TLS 入口需按你的集群自行补齐
