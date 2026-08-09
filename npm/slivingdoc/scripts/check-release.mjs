#!/usr/bin/env node
// scripts/check-release.mjs — the npm publication gate.
//
// Verifies that the GitHub release for this package's version contains every
// required artifact, the SHA256SUMS checksum file, and the license NOTICE
// (architecture section 21). Runs automatically before `npm publish`; a
// release that is missing any required asset blocks publication, so npm can
// never precede the complete GitHub release.
//
// Usage: node scripts/check-release.mjs
// The release base is overridable with SLIVINGDOC_RELEASE_BASE (tests and
// mirrors).

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { headStatus } from "../lib/download.mjs";
import { releaseBaseUrl, version } from "../lib/launcher.mjs";
import { SUPPORTED_TARGETS } from "../lib/platform.mjs";

const PKG = JSON.parse(
	readFileSync(join(fileURLToPath(new URL("..", import.meta.url)), "package.json"), "utf8"),
);

function requiredAssets() {
	const v = PKG.version;
	return [
		...SUPPORTED_TARGETS.map(({ os, arch }) => `slivingdoc-v${v}-${os}-${arch}${os === "windows" ? ".exe" : ""}`),
		"SHA256SUMS",
		"NOTICE",
	];
}

async function main() {
	const tag = `v${version()}`;
	const base = releaseBaseUrl();
	const missing = [];
	for (const asset of requiredAssets()) {
		const status = await headStatus(new URL(`${base}/${tag}/${asset}`));
		if (status !== 200) {
			missing.push(asset);
		}
	}
	if (missing.length > 0) {
		console.error(`slivingdoc: release ${tag} is incomplete; missing: ${missing.join(", ")}`);
		console.error("slivingdoc: npm publication must not precede a complete GitHub release");
		process.exit(1);
	}
	console.log(`slivingdoc: release ${tag} contains all ${requiredAssets().length} required assets`);
}

main().catch((err) => {
	console.error(`slivingdoc: release check failed: ${err.message}`);
	process.exit(1);
});
