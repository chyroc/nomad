import AppKit
import SwiftUI

struct SidebarView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            Color.clear.frame(height: 47)
            header
                .padding(.horizontal, 16)
                .frame(height: 30)
            ScrollView(.vertical) {
                VStack(alignment: .leading, spacing: 0) {
                    navigation
                    projectsSection
                }
                .padding(.top, 8)
                .padding(.bottom, 16)
            }
            .scrollIndicators(.automatic)
            footer
        }
        .background(Theme.sidebarBackground)
    }

    private var header: some View {
        HStack(spacing: 4) {
            Menu {
                Button(tr("Open folder…")) { model.pickProjectFolder() }
                Divider()
                Button(tr("Sign out")) { model.logout() }
            } label: {
                HStack(spacing: 5) {
                    Text("Nomad")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundStyle(Theme.textPrimary)
                    Image(systemName: "chevron.down")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(Theme.textSecondary)
                }
            }
            .plainMenu()
            Spacer()
            IconButton(symbol: "magnifyingglass", help: tr("Search"), size: 14) {}
            IconButton(symbol: "bell", help: tr("Notifications"), size: 14) {}
        }
    }

    private var navigation: some View {
        VStack(spacing: 0) {
            SidebarRow(symbol: "square.and.pencil", title: tr("New chat"), selected: false) { model.newChat() }
            SidebarRow(glyph: .pullRequest, title: tr("Pull requests"), selected: false) {}
            SidebarRow(symbol: "clock", title: tr("Scheduled"), selected: false) {}
            SidebarRow(symbol: "at", title: tr("Plugins"), selected: false) {}
        }
    }

    private var projectsSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            SectionHeader(title: tr("Projects")) {
                Button { model.pickProjectFolder() } label: {
                    Image(systemName: "folder.badge.plus")
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.iconMuted)
                }
                .buttonStyle(.plain)
                .help(tr("Add project"))
            }
            .padding(.top, 20)
            ForEach(model.projects) { project in
                ProjectGroup(project: project)
            }
        }
    }

    private var footer: some View {
        HStack(spacing: 8) {
            Image(systemName: "gearshape")
                .font(.system(size: 14))
                .foregroundStyle(Theme.icon)
                .frame(width: 16)
            Text(tr("Settings"))
                .font(Theme.sidebarFont)
                .foregroundStyle(Theme.textPrimary)
            Spacer()
        }
        .padding(.horizontal, 16)
        .frame(height: 46)
    }
}

struct SectionHeader<Accessory: View>: View {
    var title: String
    @ViewBuilder var accessory: () -> Accessory
    @State private var hover = false

    var body: some View {
        HStack {
            Text(title)
                .font(Theme.sectionFont)
                .foregroundStyle(Theme.textSection)
            Spacer()
            accessory().opacity(hover ? 1 : 0)
        }
        .padding(.leading, 16)
        .padding(.trailing, 20)
        .frame(height: 31)
        .contentShape(Rectangle())
        .onHover { hover = $0 }
    }
}

struct SidebarRow: View {
    var symbol: String?
    var glyph: Glyph?
    var title: String
    var indent: CGFloat = 0
    var selected: Bool
    var muted = false
    var running = false
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                if let glyph {
                    GlyphView(glyph: glyph, size: 15)
                        .frame(width: 16)
                } else if let symbol {
                    Image(systemName: symbol)
                        .font(.system(size: 14))
                        .foregroundStyle(Theme.icon)
                        .frame(width: 16)
                }
                Text(title)
                    .font(Theme.sidebarFont)
                    .foregroundStyle(muted ? Theme.textTertiary : Theme.textPrimary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 4)
                if running {
                    ProgressView().controlSize(.mini).scaleEffect(0.8)
                }
            }
            .padding(.leading, 8 + indent)
            .padding(.trailing, 8)
            .frame(height: 29)
            .background(
                RoundedRectangle(cornerRadius: 8)
                    .fill(selected ? Theme.selectedRow : (hover ? Theme.hoverRow : .clear))
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.leading, 8)
        .padding(.trailing, 23)
        .padding(.vertical, 1)
        .onHover { hover = $0 }
    }
}

struct ProjectGroup: View {
    @EnvironmentObject var model: AppModel
    var project: Project
    private let visibleLimit = 5

    var body: some View {
        let threads = model.threads(in: project.path)
        let collapsed = model.collapsedProjects.contains(project.path)
        let showAll = model.expandedThreadLists.contains(project.path)
        let shown = showAll ? threads : Array(threads.prefix(visibleLimit))
        VStack(spacing: 0) {
            SidebarRow(
                symbol: "folder",
                title: project.name,
                selected: model.route == .home && model.selectedProject == project.path
            ) {
                if model.route == .home && model.selectedProject == project.path {
                    if collapsed { model.collapsedProjects.remove(project.path) } else { model.collapsedProjects.insert(project.path) }
                } else {
                    model.newChat(in: project.path)
                }
            }
            .contextMenu {
                Button(tr("New chat")) { model.newChat(in: project.path) }
                Button(tr("Reveal in Finder")) {
                    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: project.path)])
                }
                Divider()
                Button(tr("Remove project")) { model.removeProject(project.path) }
            }
            if !collapsed {
                ForEach(shown) { t in
                    SidebarRow(
                        symbol: nil,
                        title: t.title.isEmpty ? tr("New chat") : t.title,
                        indent: 24,
                        selected: model.currentThreadID == t.id,
                        running: t.running == true || model.conversations[t.id]?.running == true
                    ) {
                        model.navigate(.thread(t.id))
                    }
                }
                if threads.count > visibleLimit {
                    SidebarRow(symbol: nil, title: showAll ? tr("Show less") : tr("Show more"), indent: 24, selected: false, muted: true) {
                        if showAll { model.expandedThreadLists.remove(project.path) } else { model.expandedThreadLists.insert(project.path) }
                    }
                }
            }
        }
    }
}
