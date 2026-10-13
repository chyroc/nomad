import Foundation

/// Looks up the user-facing copy for an English source string.
func tr(_ key: String) -> String {
    L10n.isChinese ? (L10n.zh[key] ?? key) : key
}

/// Formats a localized template, replacing `%@` placeholders in order.
func tr(_ key: String, _ args: String...) -> String {
    var out = tr(key)
    for arg in args {
        if let r = out.range(of: "%@") {
            out.replaceSubrange(r, with: arg)
        }
    }
    return out
}

enum L10n {
    static var isChinese: Bool = {
        if let forced = ProcessInfo.processInfo.environment["NOMAD_LANG"] {
            return forced.hasPrefix("zh")
        }
        return Locale.preferredLanguages.first?.hasPrefix("zh") ?? false
    }()

    static let zh: [String: String] = [
        "New chat": "新对话",
        "Pull requests": "Pull Request",
        "Scheduled": "已安排",
        "Plugins": "插件",
        "Projects": "项目",
        "Recent": "最近",
        "Show more": "展开显示",
        "Show less": "收起",
        "Settings": "设置",
        "Search": "搜索",
        "Notifications": "通知",
        "Open folder…": "打开文件夹…",
        "Add project": "添加项目",
        "Remove project": "移除项目",
        "Reveal in Finder": "在访达中显示",
        "What should we build in %@?": "你想让我们在 %@ 中构建什么？",
        "What should we build?": "你想让我们构建什么？",
        "Choose a project": "选择项目",
        "No project": "不使用项目",
        "Explore and understand code": "探索并理解代码",
        "Build a new feature, app or tool": "构建新功能、应用或工具",
        "Review code and suggest changes": "审查代码并提出修改建议",
        "Fix issues and failures": "修复问题和失败",
        "Explore this codebase and explain how it is structured.": "探索这个代码库并解释它的结构。",
        "Build ": "构建",
        "Review the current changes and suggest improvements.": "审查当前改动并提出修改建议。",
        "Find and fix the failing tests.": "找出并修复失败的测试。",
        "Ask anything": "随心输入",
        "Ask for follow-up changes": "要求后续修改",
        "Local": "本地",
        "Ask for approval": "请求批准",
        "Auto-accept edits": "自动接受编辑",
        "Plan mode": "计划模式",
        "Full access": "完全访问",
        "Ask before running commands and editing files": "运行命令和编辑文件前先询问",
        "Edit files without asking, ask before commands": "直接编辑文件，运行命令前询问",
        "Read-only planning, no changes": "只读规划，不做修改",
        "Run anything without asking": "无需询问，直接执行",
        "Model": "模型",
        "Reasoning": "推理强度",
        "Attach image": "附加图片",
        "Send": "发送",
        "Stop": "停止",
        "Worked for %@": "用时 %@",
        "Working": "正在处理",
        "Thinking": "正在思考",
        "Thought": "已思考",
        "Ran %@": "已运行 %@",
        "Running %@": "正在运行 %@",
        "Read %@": "已读取 %@",
        "Reading %@": "正在读取 %@",
        "Created %@": "已创建 %@",
        "Creating %@": "正在创建 %@",
        "Edited %@": "已编辑 %@",
        "Editing %@": "正在编辑 %@",
        "Searched %@": "已搜索 %@",
        "Searching %@": "正在搜索 %@",
        "Called %@": "已调用 %@",
        "Calling %@": "正在调用 %@",
        "Copy": "复制",
        "Copied": "已复制",
        "Interrupted": "已中断",
        "Allow Nomad to run %@?": "是否允许 Nomad 运行 %@？",
        "Yes": "是",
        "Yes, and don't ask again for %@ this session": "是，且本会话中不再询问 %@",
        "No, tell Nomad what to do differently": "否，告诉 Nomad 如何调整",
        "Sign in to Nomad": "登录 Nomad",
        "Nomad runs coding agents on Volcengine Ark managed agents. Sign in once in the browser, then paste the authorization code here.":
            "Nomad 基于火山引擎 Ark Managed Agents 运行编码智能体。在浏览器中完成授权后，把授权码粘贴到这里。",
        "Sign in with Volcengine": "使用火山引擎登录",
        "Waiting for browser authorization…": "等待浏览器授权…",
        "Paste the authorization code": "粘贴授权码",
        "Continue": "继续",
        "Open the authorization page again": "重新打开授权页面",
        "Starting nomad…": "正在启动 nomad…",
        "Could not start nomad": "无法启动 nomad",
        "Retry": "重试",
        "Sign out": "退出登录",
        "Yesterday": "昨天",
        "Rename": "重命名",
        "Starting session…": "正在创建会话…",
        "Today": "今天",
        "No threads yet": "暂无对话",
        "Toggle sidebar": "切换侧边栏",
        "Back": "返回",
        "Forward": "前进",
    ]
}
