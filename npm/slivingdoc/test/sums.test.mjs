// test/sums.test.mjs — the strict SHA256SUMS grammar (architecture 21).
import test from "node:test";
import assert from "node:assert/strict";
import { parseSums } from "../lib/sums.mjs";

const A = "a".repeat(64);
const B = "b".repeat(64);

test("parses valid sorted LF lines with the two-space separator", () => {
	const sums = parseSums(`${A}  alpha\n${B}  beta\n`);
	assert.equal(sums.get("alpha"), A);
	assert.equal(sums.get("beta"), B);
});

test("rejects a single-space separator", () => {
	assert.throws(() => parseSums(`${A} alpha\n`), /malformed SHA256SUMS line/);
});

test("rejects an uppercase digest", () => {
	assert.throws(() => parseSums(`${A.toUpperCase()}  alpha\n`), /malformed SHA256SUMS line/);
});

test("rejects a short digest", () => {
	assert.throws(() => parseSums(`${"a".repeat(63)}  alpha\n`), /malformed SHA256SUMS line/);
});

test("rejects text without a trailing LF", () => {
	assert.throws(() => parseSums(`${A}  alpha`), /LF-terminated/);
});

test("rejects an empty file", () => {
	assert.throws(() => parseSums(""), /non-empty/);
});

test("rejects an interior empty line", () => {
	assert.throws(() => parseSums(`${A}  alpha\n\n${B}  beta\n`), /malformed SHA256SUMS line/);
});

test("rejects duplicate asset names", () => {
	assert.throws(() => parseSums(`${A}  alpha\n${B}  alpha\n`), /duplicate SHA256SUMS entry/);
});
