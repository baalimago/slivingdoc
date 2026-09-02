# eslov.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-20): SUCCESS, 34 protocol documents recorded (2022-02-28 .. 2026-06-15)

## Site structure / entry
- Eslöv runs WordPress (Municipio theme). KF minutes live on the static page
  **"Fullmäktiges kallelser och protokoll"**:
  https://eslov.se/kommun-politik/kommunens-organisation/kommunfullmaktige/fullmaktiges-sammantraden-och-protokoll/
  (reachable from Kommun och politik -> Kommunens organisation -> Kommunfullmäktige ->
  "Fullmäktiges kallelser och protokoll").
- The page is a plain HTML list (no pagination, no JS needed) holding EVERY kallelse+protokoll
  pair from 2020 onward, newest first. Each meeting row: kallelse PDF, webbtv link
  (eslov.webbtvkf.se/?YYYYMMDD), protocol PDF. Filenames are under
  https://eslov.se/app/uploads/ (old captures use /wp-content/uploads/ - the path changed,
  current live path is /app/uploads/).
- Protocol filename patterns by year: 2022-2023 "protokoll-kf-YYYY-MM-DD.pdf";
  2024 mixed ("kf-2024-02-26.pdf", "protokoll-kf-2024-04-29.pdf",
  "kf-protokoll-2024-05-27-slutlig.pdf", "protokoll-kf-for-utskrift.pdf" [=2024-11-25],
  "kommunfullmaktiges-protokoll-2024-12-16.pdf"); 2025+ "kommunfullmaktiges-protokoll-YYYY-MM-DD.pdf".
- slim_http on the page with required_tokens ["app/uploads","pdf"] + max_lines 200 returns all
  document links in one shot. download_document GET 200 application/pdf works directly.
- Anslagstavla (https://eslov.se/anslag) has a filter type "Kommunfullmäktiges sammanträde (14)"
  (GET https://eslov.se/anslag/?s=&from=&to=&anslagstyp%5B%5D=kommunfullmaktiges-sammantrade);
  posts are notices (kallelser/inställt-announcements), they link to the same PDFs — NOT to be
  recorded (agenda/notice). Sidebar note: "Ibland utelämnas vissa paragrafer, ibland hela
  protokollet" (sometimes whole protocols are withheld for GDPR/secrecy).
- Site search (/?s=) returns no hits for "kommunfullmäktiges protokoll" — dead end.

## Recorded (one per meeting, all verified SAMMANTRÄDESPROTOKOLL Kommunfullmäktige by document_to_text)
- 2022 (8): 02-28, 04-25, 05-30, 06-20, 09-26, 10-31, 11-28, 12-19. 2022-10-31 main doc =
  protokoll-kf-2022-10-31-c2a7c2a7-87-9496-107-1.pdf (§§87-94,96-107); skip protokoll-kf-2022-10-31-c2a7-95.pdf (§95 partial).
- 2023 (7): 04-24, 05-29, 06-19, 09-25, 10-23, 11-27 (full); 12-18 ONLY
  protokoll-kf-2023-12-18-c2a7-103.pdf (single §103, "Paragrafen justeras omedelbart"; meeting was
  18:00-18:20) — recorded as the only minutes doc for that date, confidence 0.7. 2023-04-24 has a
  §20 omedelbar-justering partial too — skip it, use protokoll-kf-2023-04-24.pdf.
- 2024 (8): 02-26 (kf-2024-02-26.pdf), 04-29, 05-27 (kf-protokoll-2024-05-27-slutlig.pdf), 06-17,
  09-30, 10-28, 11-25 (protokoll-kf-for-utskrift.pdf, link text just "Protokoll", content verified
  = 2024-11-25 §§100-112 budget meeting), 12-16.
- 2025 (8): 02-24, 03-31, 04-28, 06-16, 09-29, 10-27, 11-24, 12-15.
- 2026 (3): 02-23, 04-27, 06-15 (06-15 within range end 2026-08-20; no later meetings in range).
- Total 34 records, confidence 0.95 (0.9 for 2024-11-25 generic filename; 0.7 for 2023-12-18 §103 partial).

## Meetings held with NO protocol published (verified — do not re-hunt)
- 2023-02-27: meeting held (kallelse-kf-2023-02-27-inkl-handlingar_1.pdf + webbtv ?20230227 exist)
  but NO protocol PDF was ever published: absent from live page (2024-02-22 Wayback capture of the
  listing shows kallelse+webbtv only) and absent from Wayback CDX of app/uploads protokoll-kf* files.
- 2023-12-18: full protocol never published, only the §103 immediate-justering doc above.
- Cancelled (no meeting, no protocol): 2022-01-31 & 2022-03-28 (2022 schedule "OBS! Inställt"),
  2023-01-30 & 2023-03-27, 2024-01-29 (noted in 2024-02-26 §19), 2025-01-27 & 2025-05-26,
  2026-01-26 & 2026-03-30 & 2026-05-25 (anslag "är inställt med anledning av för få ärenden").
- Eslöv KF meeting pattern: last Monday of month, no July/August; January usually inställt.

## Wayback / verification notes
- CDX for old uploads: https://web.archive.org/cdx/search/cdx?url=eslov.se/app/uploads/&matchType=prefix&fl=original,timestamp,statuscode&collapse=urlkey&filter=original:.*(protokoll-kf|kf-protokoll|kommunfullmaktiges-protokoll).*&filter=statuscode:200 — clean list of KF protocol files (no /wp/v2/ spam; use the uploads prefix, NOT matchType=domain which floods with wp/v2 garbage).
- Old captures of the listing page (17 captures 2020-10-23..2026-03-17) confirm the 2022/2023 schedules and that 2023-02-27 protocol was never linked. Note old path /wp-content/uploads/ in pre-2024 captures.
- 2026-04-27 and 2026-06-15 protocols are newer than the last Wayback crawl (2026-03) — they exist only live; verified by direct download.

## Tips for next run
- Re-fetch the one listing page, filter for "app/uploads"+"pdf", take the protocol link per meeting
  (skip "Kallelse", skip "§"/"omedelbar justering" partials, skip "Valberedningens protokoll").
- If a meeting's full protocol is missing from the page, check the anslag archive and Wayback CDX
  before concluding it was withheld.
