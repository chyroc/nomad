import SwiftUI

/// Line glyphs that SF Symbols lacks, drawn on a 24×24 grid.
enum Glyph {
    case pullRequest
    case branch

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        func circle(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat) -> Path {
            Path(ellipseIn: CGRect(x: rect.minX + (x - r) * s, y: rect.minY + (y - r) * s, width: 2 * r * s, height: 2 * r * s))
        }
        var path = Path()
        switch self {
        case .pullRequest:
            path.addPath(circle(6, 5, 2.6))
            path.addPath(circle(6, 19, 2.6))
            path.addPath(circle(18, 19, 2.6))
            path.move(to: p(6, 7.6))
            path.addLine(to: p(6, 16.4))
            path.move(to: p(18, 16.4))
            path.addLine(to: p(18, 9))
            path.addQuadCurve(to: p(15, 6), control: p(18, 6))
            path.addLine(to: p(11, 6))
            path.move(to: p(13.5, 3.5))
            path.addLine(to: p(11, 6))
            path.addLine(to: p(13.5, 8.5))
        case .branch:
            path.addPath(circle(6, 19, 2.6))
            path.addPath(circle(6, 5, 2.6))
            path.addPath(circle(18, 6, 2.6))
            path.move(to: p(6, 7.6))
            path.addLine(to: p(6, 16.4))
            path.move(to: p(18, 8.6))
            path.addQuadCurve(to: p(8.4, 17.6), control: p(18, 16))
        }
        return path
    }
}

struct GlyphView: View {
    var glyph: Glyph
    var size: CGFloat = 16
    var color: Color = Theme.icon

    var body: some View {
        GlyphShape(glyph: glyph)
            .stroke(color, style: StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
            .frame(width: size, height: size)
    }
}

struct GlyphShape: Shape {
    var glyph: Glyph
    func path(in rect: CGRect) -> Path { glyph.path(in: rect) }
}

extension View {
    /// Renders a Menu with its label exactly as drawn, without button chrome.
    func plainMenu() -> some View {
        self.menuStyle(.button)
            .buttonStyle(.plain)
            .menuIndicator(.hidden)
            .fixedSize()
    }
}
