# falkoping.se retrieval log (Falköpings kommun), kf = Kommunfullmäktige

## kf — round 1 (2026-08-20): 34 protocols recorded (2022-02-28 .. 2026-05-25)

## Entry page / structure
- www.falkoping.se is SiteVision CMS. KF page:
  https://www.falkoping.se/kommun--politik/kommunfullmaktige-kommunstyrelse-och-namnder/kommunfullmaktige
  (root -> Kommun & politik -> Kommunfullmäktige, kommunstyrelse och nämnder -> Kommunfullmäktige).
- Page sections: "Kallelser med föredragningslista och handlingar" (agendas - SKIP) and "Protokoll"
  (the minutes - harvest). Current year (2026) documents are listed directly as /download/ PDFs;
  earlier years are accordion folders (buttons "YYYY, öppna mapp") served by the SiteVision webapp:
  GET https://www.falkoping.se/appresource/4.1033c12f16e40c4324916bfd/<portlet>/files?folderId=<id>&svAjaxReqParam=ajax
  -> JSON {"files":[{name,id,url}]}. Folder ids (live, Aug 2026):
  - Kallelse 2025 19.7bdef21a19ac5f61f01317a (portlet ...8255e)
  - Kallelse 2024 19.776f65f5193732fee2e32e8, Kallelse 2023 19.5bb77df17c09fd50dcb882
  - Protokoll 2025 19.7bdef21a19ac5f61f013172, Protokoll 2024 19.776f65f5193732fee2e32ea,
    Protokoll 2023 19.227c6b2f184ecc001bd2439 (portlet ...82569)
  - Valberedning 2025/2024/2023 19.7bdef21a19ac5f61f013176 / 19.776f65f5193732fee2e32ee /
    19.227c6b2f184ecc001bd243b (portlet ...8259e) - SKIP (valberedning is not KF fullmäktige)
  - BÖPO 2025 19.7bdef21a19ac5f61f013178 - SKIP (mötesanteckningar)
- The webapp only keeps 3 years back (2023-2026). There is NO 2022 folder on the live site; the
  old 2022 download URLs (node ids 18.734d830e..., 18.753569e2..., 18.b0531551..., 18.37394e4f...,
  18.7e47bf79...) now 404 (GET and HEAD).

## 2023-2026 harvest (live falkoping.se)
- Protocol download URLs are /download/<18.nodeid>/<unixms>/<url-encoded filename>.pdf; date only
  in the filename. One meeting = one protocol; skip the partial "§ N" / "omedelbar justering"
  documents (e.g. 2026-04-27 §54, 2025-03-31 §44, 2023-01-30 §8) and all Kallelse/Handlingar docs.
- Counts (meeting dates): 2023: 8 (01-30, 03-27, 04-24, 05-29, 06-26, 09-25, 10-30, 11-27; 08-28
  inställt), 2024: 7 in folder (01-29, 03-25, 05-27, 06-24, 09-30, 10-28, 12-02), 2025: 9
  (01-27, 03-31, 04-28, 05-26, 06-23, 08-25, 09-29, 10-27, 12-01), 2026 in range: 4 (01-26,
  03-30, 04-27, 05-25; 06-22 inställt, 07-09 no meetings; 09-07+ out of range).
- Cross-check with the kallelse folders (agendas per meeting) confirms no protocol is missing
  from the folder listings except the two 2024 meetings below.

## 2024-04-29 and 2024-08-26 protocols are NOT on the KF page folders
- Both meetings have kallelse/handlingar in the live Kallelse folder but NO protocol in the live
  Protokoll folder (2024 folder has only 7 of 9). Wayback page captures of the old-format KF page
  (2024-07-22, 2024-08-20) also lack them. They ARE in the Ciceron diarium behind the anslagstavla:
- anslagstavlan.falkoping.se is a Ciceron JSON-RPC app (POST https://anslagstavlan.falkoping.se/json,
  session via CiceronsokServer:Test). Meeting archive query (works like lysekil):
  CiceronsokServer:ReadObject {"search_id":"...","param":"{\"t\":\"1\",\"i\":\"Kommunfullmäktige\",\"n\":\"KS\",\"today\":\"0\"}"}
  -> 81 hits (KF meetings 2014..2026, newest first; KF meetings are registered under diary code KS).
  CiceronsokServer:ReadItems {search_id, offset, limit} -> meeting rows
  CiceronsokServer:ReadObjectDetails {search_id, id:"<index>"} -> {"documents":[{name,id,filename_b64}], "items":[...]}.
- The two missing protocols: id 20 (2024-04-29) doc id 2732 and id 17 (2024-08-26) doc id 61538,
  both "Protokoll KF ... med signaturer" (filename_b64 = base64 of the .pdf name).
- Download: https://anslagstavlan.falkoping.se/download/document?filename=<b64>&id=<doc_id>.
  NOTE: HEAD returns 404 -> download_document fails; use Playwright download (anchor click +
  waitForEvent('download'), read path). Verified both PDFs (SAMMANTRÄDESPROTOKOLL KF).
- Diarium only keeps documents for some meetings (2023-09/10, 2024, few 2025 kallelser); 2022
  meetings (ids 32-42) have documents:[].

## 2022 protocols - only via Wayback Machine
- 2022 KF meetings (sammanträdesdagar 2022, from Wayback 20220320050152): 31 jan INSTÄLLT,
  28 feb, 28 mar, 25 apr, 30 maj, 27 jun, 29 aug, 26 sep, 31 okt, 28 nov, 12 dec (10 held).
- Live site has none (404). Diarium has none. Wayback captured only the Feb-May 2022 protocol PDFs
  (crawl 2022-08-19), and the Feb one (crawl 2022-03-19) is complete while Mar-May are TRUNCATED
  at exactly 1048576 bytes (1 MiB) - partial captures; pdftotext fails on them.
- Wayback URL form that returns the real PDF: https://web.archive.org/web/<ts>if_/<original url>
  (the plain /web/<ts>/ form returns the toolbar wrapper HTML).
- Captures: 2022-02-28 @20220319183624 (complete, 47p, text-verified), 2022-03-28 @20220819082505,
  2022-04-25 @20220819084910, 2022-05-30 @20220819084759 (all truncated).
- 2022-06-27, 08-29, 09-26, 10-31, 11-28, 12-12 protocols: NO capture exists (CDX exact queries
  empty; domain-wide urlkey scans for 2022-11/2022-12 show nothing) - unrecoverable via public web.
- Old-format page capture listing all 2022 protocols:
  http://web.archive.org/web/20221202153716/https://www.falkoping.se/kommun--politik/kommunfullmaktige-kommunstyrelse-och-namnder/kommunfullmaktige

## Dead ends
- anslagstavlan.falkoping.se billboard shows only currently posted items ("Justerade protokoll",
  doctype 128; "Visa fler" just adds a few more weeks); no history.
- Site search (/om-webbplatsen/sok-pa-webbplatsen?query=...) does not index the /download/ PDFs;
  "Protokoll kommunfullmäktige 2022" gives 1 hit (the KF page itself).
- Old folder-param URLs (kommunfullmaktige?folder=<oldid>&sv.url=...) render the current page and
  the old folder ids return {"files":[]} from the current appresource; Wayback never captured them.
- falkoping-sok.ciceron.cloud does not resolve; the only Ciceron instance is anslagstavlan.falkoping.se.

## Next run advice
- For 2023-2026: re-fetch the four Protokoll folder ids above (or open the KF page accordions) and
  the 2026 direct list; skip §N/omedelbar justering docs; record one per meeting date.
- For the two 2024 gaps (04-29, 08-26): re-check the diarium ReadObjectDetails for their meeting
  ids (doc id 2732 / 61538) and download via Playwright (HEAD-404 quirk).
- 2022: only Wayback copies exist; consider asking the municipal office for the missing 6 protocols.
