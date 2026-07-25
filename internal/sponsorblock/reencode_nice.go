package sponsorblock

// reencodeNice is the fixed Linux nice value for SponsorBlock re-encode ffmpeg
// (keep segments + info cards). Not a CPU percentage; copy-cut / remux / pack
// leave scheduler priority alone. Non-Linux builds no-op applyReencodeNice.
const reencodeNice = 10
