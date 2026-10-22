# Customer fix email templates

Replace every `<...>` placeholder from the resolved user evidence before
handing off. Both templates assume sending from `support@grix.im`, so the
customer can reply directly — keep that line accurate.

## 1. Windows: connector startup crash (`spawn wmic ENOENT`, 3.x)

Subject: `【需您操作】Grix 连接器在 Windows 上无法启动的修复办法（约 3 分钟）`

> 您好：
>
> 我们监测到您电脑 **<HOST>** 上的 Grix 连接器（版本 <VERSION>）近期反复启动失败，您本机的 <N> 个智能体（<AGENT NAMES>）目前处于离线状态。
>
> **原因：** 微软在新版 Windows 11 中移除了一款名为 wmic 的系统组件，旧版连接器启动时依赖它，导致每次启动即失败。新版连接器已改用 Windows 现代接口，不再依赖该组件，手动升级一次即可彻底修复。
>
> **请按以下步骤操作（约 3 分钟）：**
>
> 1. 在该电脑上打开 PowerShell（开始菜单搜索 `PowerShell` 即可，不需要管理员权限）；
> 2. 先停止当前反复重启的旧服务：`grix-connector stop`
> 3. 安装最新版：`npm install -g grix-connector@latest`
> 4. 用新版重新铺设并启动服务（**必须用 restart，不要用 start**，只有 restart 会把旧版在 Windows 上的运行副本一并更新）：`grix-connector restart`
> 5. 等待约 1 分钟，打开 Grix 桌面端或手机端，确认上述智能体恢复「在线」。
>
> **如果提示 npm 或 grix-connector 不是可识别的命令**，请不要自行安装其他软件，直接回复本邮件，我们会协助处理。
>
> **如果升级后仍未上线**，请按 Win+R 输入 `%LOCALAPPDATA%\grix-connector\logs`，把最新文件夹内的 `daemon.err.log` 作为附件回复本邮件，并附上执行命令时终端的报错截图，我们会继续跟进。
>
> 给您带来的不便我们深表歉意。升级完成后，该问题不会再随 Windows 更新复发。
>
> Grix 技术支持团队
> <DATE>

Notes for the drafter:

- The `restart` (not `start`) instruction is the crux: 3.x Windows services
  execute from a runtime copy, and only `restart` re-lays it. Machines whose
  automatic upgrade was `rolled_back` with `STARTUP_CRASH` are exactly this.
- Temporary workaround, only if the user cannot upgrade immediately: an
  administrator may restore wmic with
  `DISM /Online /Add-Capability /CapabilityName:WMIC~~~~`. Do not lead with it.

## 2. macOS: empty-config loop (`No config files found in ~/.grix/config`)

Subject: `【需您操作】Grix 连接器因缺少配置文件反复报错的处理办法`

> 您好：
>
> 我们监测到您的 Mac（**<HOST>**）上有一个 Grix 连接器在反复报错：它所在的 `~/.grix/config` 目录下没有任何智能体配置文件。该程序没有任何智能体在运行，不影响您其他工作，但会持续产生错误报告。请二选一处理：
>
> 1. **仍在使用 Grix**：在终端执行 `npm install -g grix-connector@latest` 后，按应用内指引重新配置智能体即可；
> 2. **已不再使用**：在终端执行 `npm uninstall -g grix-connector` 卸载，并在「系统设置 → 通用 → 登录项」中移除相关启动项，让它彻底停下来。
>
> 如不确定它是如何装上的，直接回复本邮件，我们可以协助确认。
>
> Grix 技术支持团队
> <DATE>

Notes for the drafter:

- This condition means no agents were ever configured (or the config was
  deleted) while autostart remained; current versions idle instead of
  crashing, so any machine still reporting it runs pre-fix code or a stale
  install entry.
- A host with no platform record at all cannot be matched to an account from
  the production database; expect to reach this user out-of-band or not at all,
  and say so in the handoff.
