# ostersund.se retrieval log

## kf — round 1 → 2 (2025)
- All harvested KF protocols (2022-02 → 2026-06, 40 documents) live on
  diariet.ostersund.se, a "Diariet" public document register (Ciceron-style
  platform).
- Document URLs are downloads of the shape
  https://diariet.ostersund.se/download/document?filename=<base64>&id=<n>
  with an opaque numeric id (<n> ranges ~2953 → 323892, not monotonic with
  date and not derivable from it).
- filename base64-decodes to the PDF name, e.g.
  "Protokoll Kommunfullmäktige 2026-06-15.signerad.pub.pdf". Filenames are
  inconsistent: some omit ".signerad", some are bare "Protokoll.pub.pdf"
  (ids 4870, 4057, 3773), some carry editorial suffixes ("- rättad å 101",
  "å 62 - omdelbar justerad"). No stable date key exists in the URL.
- Conclusion: no date-token URL template can express this target; the ids are
  opaque archive row ids found only via the register's search API. Use the
  ciceron mechanism against the diariet API root.
- Dead end: any plain-template guess at
  https://diariet.ostersund.se/download/document?... cannot be constructed
  from {YYYY}/{MM}/{DD} alone (base64 filename + non-date id).

## kf — round 3 (2026-08-19)
- Replay confirmed: a plain date-template download URL cannot reproduce the
  harvest; the only usable key into the documents is the Ciceron search API
  (JSON-RPC POST https://diariet.ostersund.se/json, method
  CiceronsokServer:Search with board="Kommunfullmäktige", doctype=64, then
  ReadItems/ReadObjectDetails to reach each meeting's "Protokoll ..." document,
  then /download/document?filename=<b64>&id=<n> for the PDF bytes).
- Proposal for this pair: platform_type=ciceron,
  service_url=https://diariet.ostersund.se, config={} (API root; the harness
  drives the JSON-RPC search per README.md). The 40 harvested meetings map
  one-to-one onto doctype=64 search hits for board "Kommunfullmäktige"
  (the doctype=1 "Möte" search adds one empty placeholder meeting
  "Kommunfullmäktige 2023-04-25" with no documents — it must be filtered out
  or yields no protocol document).
