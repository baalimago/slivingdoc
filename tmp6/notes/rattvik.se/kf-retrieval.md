# Rättvik (rattvik.se) - KF retrieval notes

Target: Rättviks kommun, meeting type kf (Kommunfullmäktige).

## Structure
- Site: SiteVision + an external document/file-browser Angular app at
  ess-app.rattvik.se ("Evolution" document archive). The protokoll page embeds
  two iframes fed by that app.
- Main entry: https://rattvik.se/kommun-och-politik.html -> Politik och demokrati
  -> Möten och protokoll -> Protokoll
- KF listing page (canonical, single source for KF protocols):
  https://rattvik.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/protokoll.html
  (covers protocols from 2018 and forward)
- The page embeds iframes "Protokoll beslutande" and "Protokoll rådgivande".
  The beslutande iframe lists folders; "01. Kommunfullmäktige" is the KF folder.
  Rows are rendered by an Angular app; content comes from a JSON API:
  - folders list:  https://ess-app.rattvik.se/api/folders/?tags=ProtokollBeslutande
                    (and ?tags=ProtokollickeBeslutande for advisory bodies)
  - folder content: https://ess-app.rattvik.se/api/folders/<folderId> (returns
                    year subfolders each with embedded documents, full JSON)
- KF folder id: 021c0f38-b68a-488c-bc8d-8b244c3e7100
- Document download: https://ess-app.rattvik.se/api/download/<documentId>/<folderId>
  -> serves the PDF (content-type application/pdf, content-disposition with filename).
  Observed when clicking a document row in the iframe.

## KF minutes found (2022-01-01 .. 2026-08-20) - 28 meetings, 1 doc each
2022: 02-24, 04-28, 06-09, 09-29, 11-10, 12-08
2023: 02-23, 04-27, 06-08, 09-28, 11-09, 12-07
2024: 02-22, 05-02, 06-13, 09-26, 11-07, 12-05
2025: 03-06, 05-08, 06-12, 06-26, 09-25, 11-13, 12-11
2026: 03-05, 05-07, 06-11

## Same-day duplicate versions (skip, keep the final one)
- 2023-12-07: main "Protokoll kommunfullmäktige 2023-12-07" (covers §§78-80,82-97)
  plus "Protokoll kommunfullmäktige 2023-12-07 direktjusterat" (only §81, same day).
  Recorded the main one only.
- 2024-09-26: "Protokoll [Normal justering] kommunfullmäktige 2024-09-26" (final)
  plus "Protokoll omedelbar justering kommunfullmäktige 2024-09-26" (scanned image,
  draft). Recorded the Normal justering one only.
- 2025-06-26 "[Omedelbar justering]" is a separate meeting (no other doc that date), recorded.

## Excluded / other folders
- "Ej aktuella nämnder" folder (id f3714142-b542-401e-8e83-da0ca4481794) holds only
  old 2018-2019 utskott protocols (finansutskott, allmänna utskott, personalutskott,
  samhällsbyggnadsutskott etc.) - no KF.
- Kallelser page (kallelser.html) is agendas/notices - not minutes.
- "Protokoll rådgivande" iframe = advisory bodies (BRÅ, Delaktighetsrådet, POLSAM, RUF) - not KF.

## Tips for next runs
- No need to click through the iframe: query the JSON API directly with
  /api/folders/<folderId> and derive download URLs /api/download/<docId>/<folderId>.
  All year folders + documents come back in one response (no pagination observed).
- Match date from the document name YYYY-MM-DD; record one minutes per meeting date.
- Cookie dialog on rattvik.se ("Godkänn nödvändiga kakor") must be dismissed before
  clicking iframe rows, but slim_http on ess-app.rattvik.se API needs no cookies.
- PDFs are real text PDFs (pdftotext works) except some "omedelbar justering"
  drafts which are scanned images (OCR required).
