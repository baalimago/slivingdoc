// lib/platform.mjs — platform-to-artifact mapping (architecture section 21).
//
// Release tags use v<semver>. Assets use
// slivingdoc-v<semver>-<os>-<arch> and add .exe on Windows. OS values are
// linux, darwin, and windows; architecture values are amd64 and arm64.
// Windows arm64 stays deferred (architecture section 23).

const OS_NAMES = Object.freeze({ linux: "linux", darwin: "darwin", win32: "windows" });
const ARCH_NAMES = Object.freeze({ x64: "amd64", arm64: "arm64" });

export const SUPPORTED_TARGETS = Object.freeze([
	{ os: "linux", arch: "amd64" },
	{ os: "linux", arch: "arm64" },
	{ os: "darwin", arch: "amd64" },
	{ os: "darwin", arch: "arm64" },
	{ os: "windows", arch: "amd64" },
]);

export class UnsupportedPlatformError extends Error {
	constructor(platform, arch) {
		const targets = SUPPORTED_TARGETS.map((t) => `${t.os}/${t.arch}`).join(", ");
		super(`slivingdoc is not available for ${platform}/${arch}; supported targets are ${targets}`);
		this.name = "UnsupportedPlatformError";
		this.platform = platform;
		this.arch = arch;
	}
}

export function artifactFor(platform, arch) {
	const os = OS_NAMES[platform];
	const targetArch = ARCH_NAMES[arch];
	if (!os || !targetArch || !SUPPORTED_TARGETS.some((t) => t.os === os && t.arch === targetArch)) {
		throw new UnsupportedPlatformError(platform, arch);
	}
	return { os, arch: targetArch, exe: os === "windows" };
}

export function assetName(version, { os, arch, exe }) {
	return `slivingdoc-v${version}-${os}-${arch}${exe ? ".exe" : ""}`;
}
