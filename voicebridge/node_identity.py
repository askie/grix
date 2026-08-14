"""节点身份解析：多节点部署中每个 voicebridge 实例的唯一标识。

main.py（订阅节点定向控制主题）与 healthcheck.py（探测本节点健康主题）
必须解析出同一个 node_id，因此收口到本模块。

优先级：VOICEBRIDGE_NODE_ID > POD_NAME > 主机名。
k8s 部署经 Downward API 注入 VOICEBRIDGE_NODE_ID=pod 名；本地多实例
测试用 VOICEBRIDGE_NODE_ID 区分；都未设置时退化为主机名（单机开发）。
"""

import os
import re
import socket

# NATS subject token 不允许空格与通配符，点号会切分 token；统一替换为 '-'
_SANITIZE = re.compile(r"[^A-Za-z0-9_-]")


def resolve_node_id() -> str:
    raw = (
        os.environ.get("VOICEBRIDGE_NODE_ID")
        or os.environ.get("POD_NAME")
        or socket.gethostname()
    ).strip()
    return _SANITIZE.sub("-", raw) or "node-unknown"
