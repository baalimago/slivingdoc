// lib/spawn.mjs — child execution with exact stream and status forwarding.
//
// stdio is inherited so stdin stays connected and the child's stdout and
// stderr are the caller's; the wrapper adds no bytes of its own. SIGINT and
// SIGTERM received by the wrapper are forwarded to the child. When the child
// dies, the wrapper reproduces the outcome: the child's exit code becomes
// the wrapper's, and a signal death is re-raised so waiters observe the same
// signal.

import { spawn } from "node:child_process";

export function runChild(binary, args) {
	const child = spawn(binary, args, { stdio: "inherit", windowsHide: true });

	for (const sig of ["SIGINT", "SIGTERM"]) {
		process.on(sig, () => {
			if (child.exitCode === null && child.signalCode === null) {
				child.kill(sig);
			}
		});
	}

	child.on("error", (err) => {
		console.error(`slivingdoc: failed to start ${binary}: ${err.message}`);
		process.exitCode = 1;
	});

	child.on("exit", (code, signal) => {
		if (signal) {
			// Die by the same signal so waiters observe the child's outcome;
			// the forwarding handlers above are removed first so the default
			// action applies. The timeout is a fallback for signals whose
			// default action does not terminate the process.
			process.removeAllListeners(signal);
			try {
				process.kill(process.pid, signal);
			} catch {
				process.exit(1);
			}
			setTimeout(() => process.exit(1), 100).unref();
			return;
		}
		process.exitCode = code ?? 1;
	});
}
