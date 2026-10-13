import SwiftUI

/// Visual constants measured from the reference desktop client.
public enum Theme {
    public static let sidebarWidth: CGFloat = 275
    public static let titlebarHeight: CGFloat = 45
    public static let contentWidth: CGFloat = 735
    public static let homeComposerWidth: CGFloat = 737

    static let sidebarBackground = Color(hex: 0xFCFCFC)
    static let mainBackground = Color.white
    static let selectedRow = Color(hex: 0xEFF0F0)
    static let hoverRow = Color(hex: 0xF4F4F4)
    static let separator = Color(hex: 0xEAEAEA)
    static let divider = Color(hex: 0xEDEDED)
    static let chipBar = Color(hex: 0xF4F4F4)
    static let cardBorder = Color(hex: 0xE6E6E6)
    static let composerBorder = Color(hex: 0xE3E3E3)
    static let userBubble = Color(hex: 0xF3F3F3)
    static let inlineCode = Color(hex: 0xECECEC)
    static let codeBlock = Color(hex: 0xF7F7F7)

    static let textPrimary = Color(hex: 0x0D0D0D)
    static let textBody = Color(hex: 0x1F1F1F)
    static let textSecondary = Color(hex: 0x5D5D5D)
    static let textTertiary = Color(hex: 0x8F8F8F)
    static let textSection = Color(hex: 0xA8A9A9)
    static let icon = Color(hex: 0x6B6B6B)
    static let sidebarIcon = Color(hex: 0x3F3F3F)
    static let sidebarText = Color(hex: 0x1F1F1F)
    static let iconMuted = Color(hex: 0x9A9A9A)

    static let accentOrange = Color(hex: 0xE25507)
    static let accentBlue = Color(hex: 0x2F7CF6)
    static let accentPurple = Color(hex: 0x8B5CF6)
    static let accentGreen = Color(hex: 0x16A34A)
    static let accentRed = Color(hex: 0xE5484D)
    static let diffAdd = Color(hex: 0x1A9F4B)
    static let diffDel = Color(hex: 0xD93F3F)
    static let sendDisabled = Color(hex: 0x8E8E90)
    static let sendEnabled = Color(hex: 0x1A1C1F)

    static let messageSize: CGFloat = 14
    static let sidebarFont = Font.system(size: 14)
    static let sectionFont = Font.system(size: 13)
    static let bodyFont = Font.system(size: 14)
    static let metaFont = Font.system(size: 13)
    static let monoFont = Font.system(size: 12.5, design: .monospaced)
}

extension Color {
    init(hex: UInt32, alpha: Double = 1) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            opacity: alpha
        )
    }
}
