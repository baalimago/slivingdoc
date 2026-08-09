// lib/launcher.mjs — the launcher orchestration: platform mapping, verified
// install, and child execution. The launcher never writes to stdout.
//
// The release repository and the download base are overridable through the
// environment (SLIVINGDOC_RELEASE_BASE and SLIVINGDOC_CACHE) so mirrors and
// the deterministic test fixtures can substitute the GitHub release without
// changing the launcher.

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { ensureBinary } from "./install.mjs";
import { artifactFor } from "./platform.mjs";
import { runChild } from "./spawn.mjs";

const PKG = JSON.parse(
	readFileSync(join(fileURLToPath(new URL("..", import.meta.url)), "package.json"), "utf8"),
);

export const RELEASE_REPO = "baalimago/slivingdoc";
export const DEFAULT_RELEASE_BASE = `https://github.com/${RELEASE_REPO}/releases/download`;

export function releaseBaseUrl(env = process.env) {
	// A caller-provided mirror may carry a trailing slash; the release URL is
	// built as <base>/<tag>/<asset>, so exactly one separator must remain.
	return (env.SLIVINGDOC_RELEASE_BASE || DEFAULT_RELEASE_BASE).replace(/\/+$/, "");
}

export function cacheRoot(env = process.env) {
	if (env.SLIVINGDOC_CACHE) {
		return env.SLIVINGDOC_CACHE;
	}
	if (env.npm_config_cache) {
		return env.npm_config_cache;
	}
	const home = env.HOME || env.USERPROFILE || ".";
	if (process.platform === "darwin") {
		return join(home, "Library", "Caches", "slivingdoc");
	}
	if (process.platform === "win32") {
		return join(env.LOCALAPPDATA || join(home, "AppData", "Local"), "slivingdoc", "cache");
	}
	return join(env.XDG_CACHE_HOME || join(home, ".cache"), "slivingdoc");
}

export function version() {
	return PKG.version;
}

export async function run(processLike = process) {
	const spec = artifactFor(processLike.platform, processLike.arch);
	const binary = await ensureBinary({
		version: version(),
		spec,
		cacheRoot: cacheRoot(processLike.env),
		baseUrl: releaseBaseUrl(processLike.env),
	});
	runChild(binary, processLike.argv.slice(2));
}
