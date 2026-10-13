import AppKit
import SwiftUI

struct LoginView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            NomadLogo().frame(width: 48, height: 48)
            Text(tr("Sign in to Nomad"))
                .font(.system(size: 28))
                .foregroundStyle(Theme.textPrimary)
                .padding(.top, 28)
            Text(tr("Nomad runs coding agents on Volcengine Ark managed agents. Sign in once in the browser, then paste the authorization code here."))
                .font(.system(size: 14))
                .foregroundStyle(Theme.textSecondary)
                .multilineTextAlignment(.center)
                .lineSpacing(4)
                .frame(maxWidth: 440)
                .padding(.top, 12)
            VStack(spacing: 12) {
                if model.login.inProgress {
                    if model.login.url == nil {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text(tr("Waiting for browser authorization…"))
                                .font(.system(size: 13))
                                .foregroundStyle(Theme.textTertiary)
                        }
                    } else {
                        codeEntry
                    }
                } else {
                    Button { model.startLogin() } label: {
                        Text(tr("Sign in with Volcengine"))
                            .font(.system(size: 14, weight: .medium))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 22)
                            .frame(height: 38)
                            .background(Capsule().fill(Theme.sendEnabled))
                    }
                    .buttonStyle(.plain)
                }
                if let err = model.login.error {
                    ErrorLine(text: err)
                        .frame(maxWidth: 440)
                }
            }
            .padding(.top, 28)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var codeEntry: some View {
        VStack(spacing: 10) {
            HStack(spacing: 8) {
                TextField(tr("Paste the authorization code"), text: $model.login.code)
                    .textFieldStyle(.plain)
                    .font(.system(size: 14, design: .monospaced))
                    .padding(.horizontal, 14)
                    .frame(height: 38)
                    .background(RoundedRectangle(cornerRadius: 10).stroke(Theme.composerBorder, lineWidth: 1))
                    .onSubmit { model.submitLoginCode() }
                Button { model.submitLoginCode() } label: {
                    Text(tr("Continue"))
                        .font(.system(size: 14, weight: .medium))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 18)
                        .frame(height: 38)
                        .background(Capsule().fill(Theme.sendEnabled))
                }
                .buttonStyle(.plain)
            }
            .frame(width: 440)
            if let url = model.login.url.flatMap(URL.init(string:)) {
                Button(tr("Open the authorization page again")) { NSWorkspace.shared.open(url) }
                    .buttonStyle(.link)
                    .font(.system(size: 12.5))
            }
        }
    }
}
