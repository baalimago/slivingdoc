// lib/sums.mjs — strict SHA256SUMS parsing (architecture section 21).
//
// Grammar: one LF-terminated line per asset, lowercase 64-hex SHA-256, two
// spaces, and the asset name. A malformed file is rejected outright: the
// launcher must never verify against a checksum list it cannot parse exactly.

const LINE = /^[0-9a-f]{64}  (\S+)$/;

export function parseSums(text) {
	if (typeof text !== "string" || text.length === 0 || !text.endsWith("\n")) {
		throw new Error("SHA256SUMS must be non-empty LF-terminated text");
	}
	const sums = new Map();
	for (const line of text.slice(0, -1).split("\n")) {
		const match = LINE.exec(line);
		if (!match) {
			throw new Error(`malformed SHA256SUMS line: ${JSON.stringify(line)}`);
		}
		if (sums.has(match[1])) {
			throw new Error(`duplicate SHA256SUMS entry: ${match[1]}`);
		}
		sums.set(match[1], line.slice(0, 64));
	}
	if (sums.size === 0) {
		throw new Error("SHA256SUMS contains no entries");
	}
	return sums;
}
