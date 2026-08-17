# NATS 鉴权说明

nats-server 通过 `nats.conf`（ConfigMap `aibot-nats-config`）启用账号鉴权 +
subject 级发布/订阅授权，密码不落清单，经 Secret 注入后在启动时做环境变量展开。

## 首次部署：创建 Secret

密码必须是**不含 `$` 的随机串**（`$` 会被 nats.conf 当作变量引用），推荐 hex：

```bash
NATS_BACKEND_PASSWORD="$(openssl rand -hex 32)"
NATS_VOICEBRIDGE_PASSWORD="$(openssl rand -hex 32)"

kubectl create secret generic aibot-nats-auth \
  --namespace <部署命名空间> \
  --from-literal=NATS_BACKEND_PASSWORD="$NATS_BACKEND_PASSWORD" \
  --from-literal=NATS_VOICEBRIDGE_PASSWORD="$NATS_VOICEBRIDGE_PASSWORD"
```

Secret 不存在时 nats-server 会因变量无法展开而拒绝启动（fail closed）。

## 账号与权限

| 账号 | 用途 | publish | subscribe |
| --- | --- | --- | --- |
| `aibot-backend` | 后端控制面 + JetStream 业务面客户端 | `voicebridge.>`, `ai.request.*`, `ai.embedding.generate`, `im.push.offline.*`, `im.reach.event`, `agent.notification.events`, `agent.notification.tts`, `pay.order.*`, `pay.refund.*`, `pay.reconcile.*`, `$JS.API.>`, `_INBOX.>` | `voicebridge.>`, `ai.request.*`, `ai.embedding.generate`, `im.push.offline.*`, `im.reach.event`, `agent.notification.events`, `pay.order.paid`, `_INBOX.>` |
| `voicebridge` | voicebridge worker（最小权限） | `voicebridge.transcript.>`, `voicebridge.control.error.>`, `voicebridge.control.bridge_exit`, `voicebridge.control.stop`, `voicebridge.control.health.>`, `_INBOX.>` | `voicebridge.control.>`, `voicebridge.inject.>`, `_INBOX.>` |

`aibot-backend` 的业务 subject 按后端代码实际 pub/sub 最小集合授权（stream
`AIBOT` 登记集合）；`$JS.API.>` 是 JetStream 管理面（`InitNATS` 的 stream
reconcile 与各 durable 消费者创建/拉取）所必需。`voicebridge.control.health.>`
是健康探针（healthcheck.py 与 worker 同账号）的节点定向 request 主题。

## 客户端接入

- **voicebridge**：读环境变量 `NATS_USER=voicebridge` / `NATS_PASSWORD`（键已列入
  `k8s/apps/base/voicebridge-secret.example.yaml`，由 Secret `aibot-voicebridge-env`
  经 envFrom 注入；healthcheck.py 同进程环境，无需额外配置）。
- **后端 aibot-server**：使用 `aibot-backend` 账号连接（后端侧改动与
  deployment 环境注入不在本目录范围内，需另行配套）。
- 本地开发（docker-compose 无鉴权 NATS）可不设 `NATS_USER`/`NATS_PASSWORD`，
  客户端按无凭证连接。

## 轮换密码

更新 Secret 后滚动重启 nats 与所有客户端 pod；无在线轮换机制（单实例有状态服务，
短暂中断由客户端自动重连吸收）。
