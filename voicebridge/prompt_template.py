"""语音客服 instructions 模板（混合快答策略）。

职责单一：把 agent 自带的人设/知识，包进一段稳定的"语音对话+混合快答"守则前缀。
- 前缀稳定不变 → 命中 OpenAI prompt caching，按 1/10 价计费（文档 31 §2.2）；
- agent_prompt 放后面 → owner 在 agent 配置里填产品信息/FAQ 话术（内置知识库）；
- 运行中由编排器注入的【权威资料】系统消息构成动态精修层（文档 31 §3.2）。

本模块不依赖任何第三方包，纯字符串拼接，便于单测。
"""

# 注入到对话上下文的权威资料标记。编排器注入的大脑答案以此开头，
# 与下面守则第 2 条呼应，GPT 据此识别并采信。两处必须一致。
AUTHORITATIVE_TAG = "【权威资料】"

# 稳定守则前缀（混合快答策略核心）。措辞固定以最大化缓存命中，勿随通话变动。
_STRATEGY_PREAMBLE = """你是一个实时电话语音客服助手。以下对话守则优先级最高，须严格遵守：

1. 快速响应，不冷场：用你已掌握的信息立即开口回答，宁可先给一个大致正确的回答，也不要长时间沉默、也不要总说"请稍等"。
2. 权威资料优先：对话中你会陆续收到以「{tag}」开头的系统消息，那是后台系统针对用户当前问题精确查询/计算出的结果，准确度高于你的记忆，必须以它为准。
3. 自然校正、不啰嗦：若「{tag}」与你刚说过的内容有实质性出入，用"更准确地说……""我帮你确认一下，应该是……"这样的口吻自然更正一次即可；若只是细节上的小差异，直接按权威资料往下说，不要反复纠正、不要为小事打断对话节奏。
4. 口语化、简短：这是电话语音，回答要短、口语、一次讲清一个要点，避免书面语和长篇大论。
5. 诚实：不确定或不知道时，坦诚说明并提出帮用户核实或转人工，绝不编造信息。
{language_line}

以下是你的角色设定与专属知识，在遵守上述守则的前提下据此服务用户：
----------------------------------------
{agent_prompt}"""


def _language_line(language: str) -> str:
    language = (language or "").strip()
    if not language:
        return "6. 语言：默认使用简体中文与用户对话。"
    return f"6. 语言：始终使用与「{language}」一致的语言与用户对话。"


def build_openai_instructions(agent_prompt: str, language: str = "") -> str:
    """构造 openai 语音 agent 的完整 instructions。

    agent_prompt 为空时仍返回守则前缀（保证基本可用），但实际部署应配置人设。
    """
    agent_prompt = (agent_prompt or "").strip()
    return _STRATEGY_PREAMBLE.format(
        tag=AUTHORITATIVE_TAG,
        language_line=_language_line(language),
        agent_prompt=agent_prompt or "(未配置专属人设，按通用礼貌客服处理)",
    )


def tag_authoritative(text: str) -> str:
    """给大脑答案打上权威资料标记，供 GPT 按守则第 2 条识别采信。"""
    text = (text or "").strip()
    if not text:
        return text
    if text.startswith(AUTHORITATIVE_TAG):
        return text
    return f"{AUTHORITATIVE_TAG}{text}"
