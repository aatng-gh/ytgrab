#!/usr/bin/env swift
// Draws AppIcon.icns. Best-effort: build.sh ignores failures here.
import AppKit

let outPath = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "AppIcon.icns"
let sourceImage = CommandLine.arguments.count > 2
    ? NSImage(contentsOfFile: CommandLine.arguments[2])
    : nil

func draw(_ size: CGFloat) -> Data? {
    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: Int(size),
        pixelsHigh: Int(size),
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ), let context = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = context

    if let sourceImage {
        context.imageInterpolation = .high
        sourceImage.draw(
            in: NSRect(x: 0, y: 0, width: size, height: size),
            from: NSRect(origin: .zero, size: sourceImage.size),
            operation: .copy,
            fraction: 1
        )
        context.flushGraphics()
        NSGraphicsContext.restoreGraphicsState()
        rep.size = NSSize(width: size, height: size)
        return rep.representation(using: .png, properties: [:])
    }

    let inset = size * 0.06
    let rect = NSRect(x: inset, y: inset, width: size - inset * 2, height: size - inset * 2)
    let body = NSBezierPath(roundedRect: rect, xRadius: size * 0.22, yRadius: size * 0.22)

    NSGradient(starting: NSColor(calibratedRed: 0.15, green: 0.16, blue: 0.20, alpha: 1),
               ending: NSColor(calibratedRed: 0.05, green: 0.05, blue: 0.07, alpha: 1))?
        .draw(in: body, angle: -90)

    // Downward arrow into a tray.
    let w = size, c = size / 2
    let shaft = NSBezierPath(roundedRect: NSRect(x: c - w * 0.055, y: w * 0.40,
                                                 width: w * 0.11, height: w * 0.24),
                             xRadius: w * 0.03, yRadius: w * 0.03)
    let head = NSBezierPath()
    head.move(to: NSPoint(x: c - w * 0.17, y: w * 0.42))
    head.line(to: NSPoint(x: c + w * 0.17, y: w * 0.42))
    head.line(to: NSPoint(x: c, y: w * 0.24))
    head.close()
    NSColor(calibratedRed: 1.0, green: 0.25, blue: 0.23, alpha: 1).setFill()
    shaft.fill()
    head.fill()

    let tray = NSBezierPath(roundedRect: NSRect(x: c - w * 0.24, y: w * 0.17,
                                                width: w * 0.48, height: w * 0.055),
                            xRadius: w * 0.025, yRadius: w * 0.025)
    NSColor.white.withAlphaComponent(0.85).setFill()
    tray.fill()
    context.flushGraphics()
    NSGraphicsContext.restoreGraphicsState()

    rep.size = NSSize(width: size, height: size)
    return rep.representation(using: .png, properties: [:])
}

func appendBigEndian(_ value: Int, to data: inout Data) {
    var encoded = UInt32(value).bigEndian
    withUnsafeBytes(of: &encoded) { data.append(contentsOf: $0) }
}

// An ICNS file is a small container of typed PNG chunks. Writing it directly
// avoids iconutil failures seen with otherwise valid iconsets on newer SDKs.
let representations: [(CGFloat, String)] = [
    (16, "icp4"), (32, "ic11"), (32, "icp5"), (64, "ic12"),
    (128, "ic07"), (256, "ic13"), (256, "ic08"), (512, "ic14"),
    (512, "ic09"), (1024, "ic10")
]

var chunks = Data()
for (size, type) in representations {
    guard let png = draw(size) else { exit(1) }
    chunks.append(contentsOf: type.utf8)
    appendBigEndian(png.count + 8, to: &chunks)
    chunks.append(png)
}

var icns = Data("icns".utf8)
appendBigEndian(chunks.count + 8, to: &icns)
icns.append(chunks)

do {
    try icns.write(to: URL(fileURLWithPath: outPath), options: .atomic)
} catch {
    fputs("Could not write icon: \(error)\n", stderr)
    exit(1)
}
