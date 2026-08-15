# Grix Kubernetes examples

本目录只提供**脱敏后的 base 清单示例**，方便本地或自建环境对照。

- `k8s/infra/base/`：基础设施示例（postgres / redis / nats 等）
- `k8s/apps/base/`：业务服务示例（api / ws / llm / push / pay 等）

## 边界说明

- **本公开仓库不包含**生产 overlay、真实 Secret、镜像仓库账号、发布台账或一键发版入口。
- 生产部署、区域 overlay、发布脚本与值班手册在**私有运维仓库**维护。
- 本公开仓库不保留可执行生产发布脚本；这里只保留 `base/` 清单和 `*.example.yaml` 作为无凭据示例。

## 本地试用建议

1. 复制 `*.example.yaml` 为本地 secret 清单，填入你自己的占位凭据。
2. 用 `kustomize build k8s/apps/base`（或等价工具）渲染，确认镜像、命名空间和资源名符合你的环境。
3. 镜像仓库、命名空间、域名一律替换成你自己的值；不要沿用文档里的历史示例名。

## 相关文档

- 基础设施示例说明：`k8s/infra/README.md`
- 业务服务示例说明：`k8s/apps/README.md`
