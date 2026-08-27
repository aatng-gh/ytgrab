import SwiftUI
import AppKit

/// The run log, in its own window so the main form never has to scroll.
struct LogView: View {
    @ObservedObject var log: LogStore

    var body: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 2) {
                        ForEach(Array(log.lines.enumerated()), id: \.offset) { index, line in
                            Text(line)
                                .font(.system(size: 11, design: .monospaced))
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .id(index)
                        }
                    }
                    .padding(10)
                }
                .onChange(of: log.lines.count) {
                    proxy.scrollTo(log.lines.count - 1, anchor: .bottom)
                }
            }

            Divider()
            HStack {
                Text("\(log.lines.count) line\(log.lines.count == 1 ? "" : "s")")
                    .font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button("Copy") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(log.lines.joined(separator: "\n"), forType: .string)
                }
                .buttonStyle(.bordered)
                .foregroundStyle(.primary)
                .disabled(log.lines.isEmpty)
            }
            .padding(10)
        }
        .frame(minWidth: 480, idealWidth: 620, minHeight: 300, idealHeight: 440)
    }
}
