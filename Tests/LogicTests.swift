import Foundation

// Models.swift uses this marker while building its progress arguments. The
// production Downloader is intentionally excluded from these pure logic tests.
enum Downloader {
    static let progressPrefix = "@@PROG@@"
}

@main
struct LogicTests {
    private static var failures = 0

    static func main() throws {
        check(DownloadRequest.isValidTimestamp("1:30"), "accepts m:ss")
        check(DownloadRequest.isValidTimestamp("12:04:00"), "accepts h:mm:ss")
        check(!DownloadRequest.isValidTimestamp("1:2"), "rejects incomplete seconds")

        var capped = DownloadRequest()
        capped.videoQuality = .p360
        let cappedSelector = ArgumentBuilder.videoFormatSelector(capped)
        check(cappedSelector.contains("height<=360"), "applies selected height cap")
        check(!cappedSelector.contains("bv*+ba"), "does not fall back above selected cap")

        capped.videoQuality = .best
        check(ArgumentBuilder.videoFormatSelector(capped).contains("bv*+ba"),
              "keeps uncapped fallback for Best available")

        let cookie = ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tvalue"
        check(Cookies.parsedCount(in: cookie) == 1, "recognizes Netscape cookies")
        check(Cookies.normalized(cookie).hasPrefix(Cookies.netscapeHeader + "\n"),
              "adds a missing Netscape header")

        let jar = try Cookies.writeTemporaryJar(cookie)
        check(jar != nil, "creates a temporary cookie jar")
        if let jar {
            let pidToken = "YTGrab-\(ProcessInfo.processInfo.processIdentifier)-"
            check(jar.deletingLastPathComponent().lastPathComponent.hasPrefix(pidToken),
                  "records the owner process in the jar directory")
            Cookies.shred(jar)
            check(!FileManager.default.fileExists(atPath: jar.path), "removes the cookie jar")
        }

        if failures > 0 {
            throw TestFailure(count: failures)
        }
        print("All logic tests passed")
    }

    private static func check(_ condition: @autoclosure () -> Bool, _ message: String) {
        if condition() {
            print("PASS: \(message)")
        } else {
            failures += 1
            print("FAIL: \(message)")
        }
    }

    private struct TestFailure: Error {
        let count: Int
    }
}
