"""Kubernetes probe: verify THIS node's worker answers on its node-scoped subject.

必须打节点定向主题而不是广播主题：多副本下广播 health 会被其他副本代答，
本 pod 进程卡死时探针仍通过，k8s 永远不会重启它。
"""

import asyncio
import json
import os

import nats

from node_identity import resolve_node_id

SUBJECT_HEALTH = "voicebridge.control.health"


async def main() -> None:
    nc = await nats.connect(
        os.getenv("NATS_URL", "nats://localhost:4222"),
        user=os.getenv("NATS_USER") or None,
        password=os.getenv("NATS_PASSWORD") or None,
        connect_timeout=1,
        max_reconnect_attempts=0,
    )
    try:
        subject = f"{SUBJECT_HEALTH}.{resolve_node_id()}"
        response = await nc.request(subject, b"{}", timeout=1)
        data = json.loads(response.data)
        if data.get("ok") is not True:
            raise RuntimeError("unexpected voicebridge health response")
    finally:
        await nc.close()


if __name__ == "__main__":
    asyncio.run(main())
