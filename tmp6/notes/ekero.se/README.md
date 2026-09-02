# ekero.se scanner notes (kf = Kommunfullmäktige)

## Site structure
- Public meeting listing: https://ekero.se/kommun-politik/moten-handlingar--protokoll
  - "Kallelser och protokoll" accordion (2024-10-07 onwards) is a NetPublicator
    public reader: JS calls
    https://docs.netpublicator.com/api/public/r57228845/read?hash=84d0899d695b380&isr=true
    (root channels; append channel id e.g. 84d0899d695b380-8c7e1809b03e7192092 for
    Kommunfullmäktige meetings; append meeting hash for agenda items).
    Document links look like
    https://docs.netpublicator.com/api/public/r57228845/document/{uuid}?hash=...
    Items are per-§ documents, not a single combined protocol.
- Old archive (2013-2024) AND current (2025+) live in the Angular app
  https://document.ekero.se/ (Medborgare Ekerö / Evolution document web).
  REST API (all JSON):
  - GET https://document.ekero.se/api/folders            -> root folder list
  - GET https://document.ekero.se/api/folders/{folderId} -> full recursive subtree
    (Kommunfullmäktige 2013-2024 = 3e2b4840-407a-4e86-a6e2-f061abf4f785, 9 MB;
     current "Kommunfullmäktige" = aed82619-d752-4b51-a11c-ff4672f4340b)
  - Download: https://document.ekero.se/api/download/{documentId}/{folderId}
    (observed working pattern; returns the PDF).
- Tree layout (old): KF 2013-2024 -> year -> "Kommunfullmäktige_Möte YYYY-MM-DD"
  -> "Protokoll" folder -> "Protokoll KF YYYY-MM-DD ...(.pdf)".
  Some meetings have the protocol split into multiple §-range PDFs (e.g.
  2022-05-31, 2023-03-06, 2023-06-12, 2024-10-07, 2024-12-09); pick ONE per date.
- Current layout: KF YYYY-MM-DD, kl... -> Kallelse + Protokoll folders;
  protocol documents named "Protokoll [Normal justering]" / "[Omedelbar justering]".
  Recorded [Normal justering] per meeting. Meetings 2026-04-20 and 2026-06-08
  exist as empty folders (no protocol published yet); nothing recorded.

## Recorded URLs (all https://document.ekero.se/api/download/...)
- 2022: 03-08, 04-26, 05-31, 06-21, 08-30, 10-18, 11-08, 11-22, 12-13
- 2023: 03-06, 04-24, 05-22, 06-12, 10-09, 11-06, 11-20, 12-11
- 2024: 01-22, 03-04, 04-22, 05-20, 06-10, 10-07, 11-04, 11-18, 12-09
- 2025: 03-03, 04-28, 05-19, 06-09, 10-06, 11-03, 11-17, 12-08
- 2026: 02-16
Confidence 0.95 (0.85-0.9 where protocol title date vs folder date mismatched,
e.g. 2023-04-24 folder w/ "2023-05-04" title, 2023-12-11 folder w/ "2022-12-11"
title, 2023-10-09 folder w/ "2023-10-19" title).

## Dead ends / tips
- slim_http truncates the 9 MB subtree JSON; parse it in-page with
  page.evaluate(fetch('/api/folders/...').then(r=>r.json())) and walk.
- The ekero.se NetPublicator widget and document.ekero.se overlap for
  2024-10-07..2025-12-08 and 2026-02-16; document.ekero.se has the combined
  protocol, prefer it.

## Decision extraction quirks (from combined PDF protocols)
- The combined protocol PDF's table of contents may list § numbers that are
  absent from the body (e.g. valärenden §§ 9-12 handled separately/not printed);
  extract decisions only from what the body actually contains. The anslag page
  states which § ranges the protocol covers (e.g. "§§ 1–8, 12–24").
- Opening §§ (Upprop, Val av justerare, Frågor/Interpellationer, Anmälan för
  kännedom, Ajournering) record routine "Beslut" lines; the substantive
  decisions are the policy/tax/motion §§. Judge each §'s Beslut section on its
  own, since all are formatted identically.
