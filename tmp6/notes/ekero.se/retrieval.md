# ekero.se retrieval log

## kf — round 1 (prior run, from scorecard): recall 0
- Candidate proposal failed: "failed to unmarshal JSON from all 8 candidates:
  invalid character 'Y' looking for beginning of object key string" — i.e. the
  proposal drove a JSON-parsing document-API interaction at the target and every
  candidate response was non-JSON (the Angular SPA / non-API HTML shell), so
  nothing was retrieved.

## kf — round 2 (2026-08-19): ABORT — no registered mechanism fits
- Evidence (README.md): KF protocols 2013-2026 are served from
  https://document.ekero.se/, an Angular "Medborgare Ekerö / Evolution document
  web" with a REST API (all JSON):
  - GET https://document.ekero.se/api/folders            -> root folder list
  - GET https://document.ekero.se/api/folders/{folderId} -> full recursive
    subtree (KF 2013-2024 = 3e2b4840-407a-4e86-a6e2-f061abf4f785; current KF =
    aed82619-d752-4b51-a11c-ff4672f4340b)
  - Download: https://document.ekero.se/api/download/{documentId}/{folderId}
    (both ids opaque UUIDs).
- ciceron: rejected. ciceron is defined as the Ciceron document API (JSON-RPC).
  document.ekero.se is NOT Ciceron: it is a plain REST JSON API (GET
  /api/folders...), no JSON-RPC endpoint, no CiceronsokServer-style search.
  Round 1 already demonstrated this — a JSON-parsing drive of this API root
  yielded non-JSON responses and recall 0. The NetPublicator widget
  (docs.netpublicator.com/api/public/r57228845) is also not Ciceron and only
  covers 2024-10-07+; document.ekero.se is preferred for those dates anyway.
- plain: rejected. Download URLs are /api/download/<uuid>/<uuid> — no
  {YYYY}/{YY}/{MM}/{DD} component anywhere in path or query. Meeting dates
  appear only inside folder names ("Kommunfullmäktige_Möte YYYY-MM-DD") and PDF
  titles, never in the URL. No date-token template can reproduce any harvested
  URL.
- What WOULD work, but is not a registered mechanism: walk the document.ekero.se
  folder tree via /api/folders (root -> Kommunfullmäktige -> year ->
  "Kommunfullmäktige_Möte YYYY-MM-DD" -> "Protokoll" folder), pick the one
  combined protocol per meeting date, download via
  /api/download/{documentId}/{folderId}. Use page.evaluate(fetch('/api/folders/...'))
  because slim_http truncates the 9 MB subtree JSON (README.md "Dead ends /
  tips").
- Conclusion: neither ciceron (JSON-RPC) nor plain (date-token URL template)
  expresses this pair; the matching pattern is a REST folder-tree API with
  opaque-UUID downloads, which is not among the registered mechanisms.
