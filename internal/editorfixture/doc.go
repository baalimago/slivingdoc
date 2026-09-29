// Package editorfixture is a test-only tool for the browser editor's
// TypeScript port of the pack protocol (slivingdoc-cloud, cloud/editor).
//
// Generate runs the real notebook over the in-memory object store and
// libgit2 and dumps the resulting store, key for key, as fixture
// directories: `current` plus `packs/...`, exactly the objects a hosted
// space holds, with an expected.json describing what a reader must
// reconstruct. Conform is the other direction: it runs DecodeManifest,
// ImportPack and ValidateHistory over every manifest and pack a directory
// holds, so packs and manifests the TypeScript side writes can be proved
// against the reference implementation.
package editorfixture
