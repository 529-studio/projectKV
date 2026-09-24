#!/usr/bin/env swift
import Foundation
import Vision
import AppKit

// Usage: swift extract_osd_timestamp.swift <image_path>
// Outputs JSON: {"status": "OK", "osd_text": "...", "timestamp": "...", "confidence": 1.0}

guard CommandLine.arguments.count > 1 else {
    print("{\"error\": \"Usage: extract_osd_timestamp.swift <image_path>\"}")
    exit(1)
}

let imagePath = CommandLine.arguments[1]
let url = URL(fileURLWithPath: imagePath)

guard let image = NSImage(contentsOf: url),
      let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    print("{\"error\": \"Failed to load image: \(imagePath)\"}")
    exit(1)
}

// Isolate top banner (top 40 pixels) where traffic camera OSD timestamps are burned
let h = cgImage.height
let w = cgImage.width
let bannerHeight = min(42, h)

// In CGImage, origin (0,0) is at top-left
let topBannerRect = CGRect(x: 0, y: 0, width: w, height: bannerHeight)
let bannerCg = cgImage.cropping(to: topBannerRect) ?? cgImage

// Scale 3x with high quality bicubic interpolation to clarify small 10px OSD font
let sW = bannerCg.width * 3
let sH = bannerCg.height * 3
let colorSpace = CGColorSpaceCreateDeviceRGB()
let ctx = CGContext(data: nil, width: sW, height: sH, bitsPerComponent: 8, bytesPerRow: sW * 4, space: colorSpace, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
ctx.interpolationQuality = .high
ctx.draw(bannerCg, in: CGRect(x: 0, y: 0, width: sW, height: sH))
let scaledBanner = ctx.makeImage() ?? bannerCg

let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.usesLanguageCorrection = false

let handler = VNImageRequestHandler(cgImage: scaledBanner, options: [:])
do {
    try handler.perform([request])
} catch {
    print("{\"error\": \"Vision request failed: \(error.localizedDescription)\"}")
    exit(1)
}

var observations = request.results ?? []
if observations.isEmpty {
    let fullHandler = VNImageRequestHandler(cgImage: cgImage, options: [:])
    try? fullHandler.perform([request])
    observations = request.results ?? []
}

guard !observations.isEmpty else {
    print("{\"status\": \"NO_TEXT_DETECTED\"}")
    exit(0)
}

var lines: [String] = []
var detectedTime: String? = nil

// Regular expressions to match OSD timestamp formats
// Examples: 24.Sep 2026 14:48:42, 24-09-2026 14:48:42, 14:48:42
let timePattern = try! NSRegularExpression(pattern: #"(\d{1,2}[\.\-\/][A-Za-z]{3,}[\.\-\/]?\s*\d{2,4}\s+\d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2})"#, options: [])

for obs in observations {
    if let candidate = obs.topCandidates(1).first {
        let text = candidate.string.trimmingCharacters(in: .whitespacesAndNewlines)
        lines.append(text)
        
        let range = NSRange(text.startIndex..<text.endIndex, in: text)
        if detectedTime == nil, let match = timePattern.firstMatch(in: text, options: [], range: range) {
            if let r = Range(match.range, in: text) {
                detectedTime = String(text[r])
            }
        }
    }
}

let fullText = lines.joined(separator: " | ")
let result: [String: Any] = [
    "status": "OK",
    "imagePath": imagePath,
    "timestamp": detectedTime as Any,
    "raw_osd": fullText
]

if let jsonData = try? JSONSerialization.data(withJSONObject: result, options: [.prettyPrinted]),
   let jsonStr = String(data: jsonData, encoding: .utf8) {
    print(jsonStr)
}
