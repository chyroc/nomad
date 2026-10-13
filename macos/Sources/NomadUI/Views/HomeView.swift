import SwiftUI

struct HomeView: View {
    @EnvironmentObject var model: AppModel

    private var projectName: String? {
        model.selectedProject.map { ($0 as NSString).lastPathComponent }
    }

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .top) {
                hero
                    .frame(width: geo.size.width)
                    .position(x: geo.size.width / 2, y: geo.size.height / 2)
                VStack(spacing: 0) {
                    Spacer()
                    ZStack(alignment: .top) {
                        chipBar
                            .frame(width: Theme.homeComposerWidth - 27)
                        ComposerView(placeholder: tr("Ask anything"), running: false)
                            .frame(width: Theme.homeComposerWidth)
                            .padding(.top, 37)
                    }
                    .padding(.bottom, 15)
                }
                .frame(maxWidth: .infinity)
            }
        }
    }

    private var hero: some View {
        VStack(spacing: 0) {
            NomadLogo()
                .frame(width: 48, height: 48)
            titleLine
                .padding(.top, 32)
            HStack(spacing: 12) {
                SuggestionCard(symbol: "binoculars", tint: Theme.accentBlue, title: tr("Explore and understand code"),
                               prompt: tr("Explore this codebase and explain how it is structured."))
                SuggestionCard(symbol: "hammer", tint: Theme.accentPurple, title: tr("Build a new feature, app or tool"),
                               prompt: tr("Build "))
                SuggestionCard(symbol: "arrow.triangle.2.circlepath", tint: Theme.accentGreen, title: tr("Review code and suggest changes"),
                               prompt: tr("Review the current changes and suggest improvements."))
                SuggestionCard(symbol: "ladybug", tint: Theme.accentOrange, title: tr("Fix issues and failures"),
                               prompt: tr("Find and fix the failing tests."))
            }
            .padding(.top, 36)
        }
    }

    @ViewBuilder
    private var titleLine: some View {
        if let name = projectName {
            let parts = tr("What should we build in %@?").components(separatedBy: "%@")
            HStack(spacing: 0) {
                Text(parts.first ?? "")
                ProjectPicker(name: name)
                Text(parts.count > 1 ? parts[1] : "")
            }
            .font(.system(size: 28))
            .foregroundStyle(Theme.textPrimary)
        } else {
            VStack(spacing: 14) {
                Text(tr("What should we build?"))
                    .font(.system(size: 28))
                    .foregroundStyle(Theme.textPrimary)
                Menu {
                    ProjectMenuItems()
                } label: {
                    Text(tr("Choose a project"))
                        .font(.system(size: 14))
                        .foregroundStyle(Theme.accentBlue)
                }
                .plainMenu()
            }
        }
    }

    private var chipBar: some View {
        HStack(spacing: 22) {
            Menu {
                ProjectMenuItems()
            } label: {
                Chip(symbol: "folder", title: projectName ?? tr("No project"))
            }
            .plainMenu()
            Chip(symbol: "laptopcomputer", title: tr("Local"))
            if let p = model.selectedProject, let branch = model.branches[p], !branch.isEmpty {
                Chip(symbol: nil, glyph: .branch, title: branch)
            }
            Spacer()
        }
        .padding(.horizontal, 21)
        .frame(height: 50, alignment: .top)
        .padding(.top, 11)
        .background(
            UnevenRoundedRectangle(topLeadingRadius: 16, bottomLeadingRadius: 0, bottomTrailingRadius: 0, topTrailingRadius: 16, style: .continuous)
                .fill(Theme.chipBar)
        )
    }
}

struct Chip: View {
    var symbol: String?
    var glyph: Glyph?
    var title: String

    var body: some View {
        HStack(spacing: 7) {
            if let glyph {
                GlyphView(glyph: glyph, size: 13.5, color: Theme.sidebarIcon)
            } else if let symbol {
                Image(systemName: symbol)
                    .font(.system(size: 12))
                    .foregroundStyle(Theme.sidebarIcon)
            }
            Text(title)
                .font(.system(size: 13))
                .foregroundStyle(Theme.sidebarText)
        }
    }
}

struct ProjectPicker: View {
    @EnvironmentObject var model: AppModel
    var name: String

    var body: some View {
        Menu {
            ProjectMenuItems()
        } label: {
            Text(name)
                .font(.system(size: 28))
                .foregroundStyle(Theme.textPrimary)
                .overlay(alignment: .bottom) {
                    DottedLine()
                        .stroke(Theme.textTertiary, style: StrokeStyle(lineWidth: 1, dash: [2, 2]))
                        .frame(height: 1)
                        .offset(y: 2)
                }
        }
        .plainMenu()
    }
}

/// Project choices shared by every project picker, including "no project".
struct ProjectMenuItems: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        ForEach(model.projects) { p in
            Button {
                model.selectedProject = p.path
            } label: {
                if model.selectedProject == p.path { Label(p.name, systemImage: "checkmark") } else { Text(p.name) }
            }
        }
        Divider()
        Button {
            model.selectedProject = nil
        } label: {
            if model.selectedProject == nil { Label(tr("No project"), systemImage: "checkmark") } else { Text(tr("No project")) }
        }
        Button(tr("Open folder…")) { model.pickProjectFolder() }
    }
}

struct DottedLine: Shape {
    func path(in rect: CGRect) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: rect.minX, y: rect.midY))
        p.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
        return p
    }
}

struct SuggestionCard: View {
    @EnvironmentObject var model: AppModel
    var symbol: String
    var tint: Color
    var title: String
    var prompt: String
    @State private var hover = false

    var body: some View {
        Button {
            model.drafts["home"] = prompt
        } label: {
            VStack(alignment: .leading, spacing: 0) {
                Image(systemName: symbol)
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(tint)
                Spacer(minLength: 8)
                Text(CJK.breakable(title))
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(Theme.textPrimary)
                    .lineSpacing(4)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.leading, 16)
            .padding(.trailing, 12)
            .padding(.top, 17)
            .padding(.bottom, 14)
            .frame(width: 169, height: 104, alignment: .topLeading)
            .background(RoundedRectangle(cornerRadius: 13, style: .continuous).fill(hover ? Color(hex: 0xFAFAFA) : .white))
            .overlay(RoundedRectangle(cornerRadius: 13, style: .continuous).stroke(Theme.cardBorder, lineWidth: 1))
            .shadow(color: .black.opacity(0.03), radius: 2, x: 0, y: 1)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}

/// Nomad's mark: a scalloped blob outline with a prompt glyph.
struct NomadLogo: View {
    var tint: Color = Color(hex: 0xC2C2C2)
    var lineWidth: CGFloat = 2.6

    var body: some View {
        GeometryReader { geo in
            let side = min(geo.size.width, geo.size.height)
            let stroke = lineWidth * side / 48
            ZStack {
                BlobShape()
                    .stroke(tint, style: StrokeStyle(lineWidth: stroke, lineJoin: .round))
                PromptShape()
                    .stroke(tint, style: StrokeStyle(lineWidth: stroke, lineCap: .round, lineJoin: .round))
                    .frame(width: side * 0.42, height: side * 0.3)
            }
            .padding(stroke / 2)
            .frame(width: side, height: side)
        }
    }
}

struct BlobShape: Shape {
    func path(in rect: CGRect) -> Path {
        let c = CGPoint(x: rect.midX, y: rect.midY)
        let r = min(rect.width, rect.height) / 2
        var p = Path()
        let steps = 240
        for i in 0...steps {
            let t = Double(i) / Double(steps) * 2 * .pi
            let wobble = 0.07 * cos(6 * t + 0.5) + 0.02 * cos(2 * t)
            let rr = r * (0.9 + wobble)
            let pt = CGPoint(x: c.x + rr * cos(t), y: c.y + rr * sin(t))
            if i == 0 { p.move(to: pt) } else { p.addLine(to: pt) }
        }
        p.closeSubpath()
        return p
    }
}

struct PromptShape: Shape {
    func path(in rect: CGRect) -> Path {
        var p = Path()
        let w = rect.width, h = rect.height
        p.move(to: CGPoint(x: rect.minX, y: rect.minY))
        p.addLine(to: CGPoint(x: rect.minX + w * 0.32, y: rect.minY + h * 0.42))
        p.addLine(to: CGPoint(x: rect.minX, y: rect.minY + h * 0.84))
        p.move(to: CGPoint(x: rect.minX + w * 0.55, y: rect.maxY))
        p.addLine(to: CGPoint(x: rect.maxX, y: rect.maxY))
        return p
    }
}

/// Line-breaking helpers for CJK copy.
enum CJK {
    /// Inserts zero-width spaces after CJK characters so text wraps per
    /// character, as browsers do, instead of at dictionary word boundaries.
    static func breakable(_ s: String) -> String {
        var out = ""
        for ch in s {
            out.append(ch)
            if ch.unicodeScalars.contains(where: { (0x3000...0x9FFF).contains($0.value) || (0xFF00...0xFFEF).contains($0.value) }) {
                out.append("\u{200B}")
            }
        }
        return out
    }
}
