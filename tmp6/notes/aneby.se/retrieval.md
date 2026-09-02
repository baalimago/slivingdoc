# aneby.se retrieval log

## kf — round 1 (2026-08-19): ABORT — no registered mechanism fits
- Harvest evidence (README.md): KF protocols are plain PDFs at
  https://aneby.se/download/<node>/<unixms>/Protokoll%20KF%20YYYY-MM-DD.pdf,
  linked from the static SiteVision page
  https://aneby.se/sidor/kommun-och-politik/politik-och-beslut/moten-och-protokoll.html
  (one long HTML list grouped by committee heading, newest first).
- ciceron: rejected. aneby.se has no Ciceron / diarium JSON-RPC API — README.md
  states "No diarium / no JavaScript listing needed; all protocol links are plain
  <a href>". There is no JSON-RPC service root to point service_url at.
- plain: rejected. The download URL's directory part is
  /download/<18.hexnode>/<unixms>/ — an opaque SiteVision node id plus a
  publish/version timestamp in milliseconds. Neither can be derived from
  {YYYY}/{YY}/{MM}/{DD}; the date appears only inside the filename. The filename
  is not a stable date pattern either (2022 files named
  "Protokoll%20Kommunfullm%C3%A4ktige%20YYYY-MM-DD.pdf or similar", later ones
  "Protokoll%20KF%20YYYY-MM-DD.pdf"). No date-token template can reproduce any
  harvested URL, and no scorecard revision could recover the opaque path parts.
- What WOULD work, but is not a registered mechanism: parse the static listing
  page HTML and follow the <a href> on each KF row. For a future mechanism/manual
  run: listing URL = moten-och-protokoll.html above; filter on link TEXT for
  "Kommunfullmäktige" (the word "fullmäktige" alone matches every link via shared
  nav context); skip "KF presidium protokoll" and "Protokoll Valberedning KF";
  download_document works on the PDF URLs (200, application/pdf).
- Conclusion: neither ciceron nor plain expresses this pair. The matching pattern
  is "static listing page scrape", which is not among the registered mechanisms.

## kf — round 2 (retry after "none" was rejected as unregistered): ABORT confirmed
- Re-checked BOTH registered mechanisms against the harvest evidence before
  aborting, since round 1's "none" was not a legal proposal.
- ciceron: still no. aneby.se is SiteVision CMS end-to-end; no Ciceron/JSON-RPC
  service root exists on the target (no /json endpoint, no diarium subdomain, no
  JS-rendered listing — the archive is the static moten-och-protokoll.html page).
  There is no service_url value that names a Ciceron API root.
- plain: still no. Every harvested KF URL is
  https://aneby.se/download/18.<hexnode>/<unixms>/Protokoll%20KF%20YYYY-MM-DD.pdf
  (2022 variants "Protokoll%20Kommunfullm%C3%A4ktige%20YYYY-MM-DD.pdf").
  Directory segments are an opaque SiteVision node id (18.<hexnode>) and a
  per-file publish/version timestamp in Unix milliseconds; neither is a function
  of {YYYY}/{YY}/{MM}/{DD} and the date appears only in the filename segment.
  Any date-token template 404s on the opaque path parts -> recall 0 and recency
  recall 0 (the most recent 2026-06-15 protocol would be missed — fatal).
- Cross-target check (notebook-wide): Östersund's ciceron fit is valid only
  because diariet.ostersund.se is a real Ciceron diarium behind the SPA; Aneby
  has no equivalent service. Ekerö (Evolution REST API) and others also lack
  registered mechanisms — consistent with §6.4: the mechanism inventory is
  deliberately incomplete, and "static listing page scrape" is not registered.
- Final: ABORT for Aneby/kf. The only working pattern is a scrape of the static
  listing page moten-och-protokoll.html (filter <a> link TEXT for
  "Kommunfullmäktige", skip "KF presidium protokoll"/"Protokoll Valberedning KF",
  download the PDF). Requires an unregistered mechanism; no registered
  configuration (ciceron or plain) can express this pair.
