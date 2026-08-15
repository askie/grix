# Voice Bridge Worker（Python）

使用 [LiveKit Agents SDK](https://docs.livekit.io/agents/) 实现 AI 语音托管。

## 架构

```
Go ws 进程
  └─ NATSBridgeManager.StartBridge(call_id, system_prompt, voice)
       └─ NATS: voicebridge.control.start
            └─ Python voicebridge（本进程）
                 ├─ 生成 ai_bot LiveKit token
                 ├─ 连接 LiveKit 房间（call-{call_id}）
                 └─ AgentSession(llm=OpenAI Realtime)
                      ├─ 自动订阅 caller 音频轨道
                      ├─ OpenAI Realtime WS（双工音频）
                      └─ 发布 ai_bot 音频轨道

Go ws 进程（四档语音通话：接管=静音 AI，交回=恢复，session 始终保持、API 不断）
  └─ NATSBridgeManager.MuteBridge(call_id)       → control.mute     → 关 AI 音频输入+输出（保留 session）
  └─ NATSBridgeManager.UnmuteBridge(call_id)     → control.unmute   → 恢复 AI 听 + 发声（交回秒切）
  └─ NATSBridgeManager.StopBridge(call_id)       → control.stop     → session.aclose()
  └─ NATSBridgeManager.InterruptBridge(call_id)  → control.interrupt → 旧接管路径(interrupt+aclose)，已被 Mute 取代，保留备用
```

## 多节点模式

支持多副本水平扩容，路由规则：

- **start（新通话）**：广播主题 + NATS queue group（`voicebridge`）负载均衡，
  一条 start 只有一个节点处理；回执带 `node_id`（pod 名 / `VOICEBRIDGE_NODE_ID`），
  Go 侧 `NATSBridgeManager` 据此在内存中记录 call→节点 归属。
- **mute / unmute / interrupt / 重建 start**：节点定向主题 `<subject>.<node_id>`，
  精确命中持有该通话 session 的节点。重建 start 必须回到原节点
  （`_handover_rooms` 中的旧 room 须由原节点断开，避免 DuplicateIdentity）；
  定向请求失败（节点已死）时 Go 清除归属并回退 queue group 由存活节点接管。
- **stop / inject**：保持广播，非 owner 节点查不到本地 session 自动忽略。
- **广播 mute/unmute/interrupt**（Go 无归属信息时的回退）：仅 owner 应答，
  非 owner 静默（`_owner_only` 守卫），避免多副本抢答。
- **health**：k8s 探针必须打本节点定向主题 `control.health.<node_id>`
  （healthcheck.py 已内置）；广播 health 会被任意存活副本代答，仅用于
  "至少一个节点存活" 的人工排查。

## 依赖

```bash
pip install -r requirements.txt
```

## 配置

```bash
cp .env.example .env
# 填入真实值
```

## 启动

```bash
python main.py
```

## 测试

单元测试（无需外部依赖，纯逻辑）：

```bash
cd voicebridge
../.venv/bin/python -m unittest test_bridge_config test_set_ai_muted test_main \
  test_openai_orchestrator test_prompt_template -v
```

端到端测试（需本机 LiveKit + NATS）：

```bash
# 起本机基础设施
docker compose -f ../backend/docker-compose.yml up -d nats livekit
../.venv/bin/python -m unittest test_e2e -v          # 音频流路径（mock LLM）
```

GPT 真机端到端（`test_gpt_realtime_e2e.py`，连真实 OpenAI，**会计费**，默认跳过）：

```bash
# 需同时满足：GRIX_GPT_E2E=1 + OPEN_API_KEY/OPENAI_API_KEY + 本机 LiveKit/NATS
# in-process 起一个 bridge 实例（勿同时另跑 main.py，否则抢同一通话 DuplicateIdentity）
GRIX_GPT_E2E=1 ../.venv/bin/python -m unittest test_gpt_realtime_e2e -v
```

覆盖：openai 连接、接点A 转写、语序编排（create_response=False+抢答）、
接点B 注入路由、四档接管（只听不说）。
