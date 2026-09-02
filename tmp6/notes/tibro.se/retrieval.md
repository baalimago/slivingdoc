# tibro.se scanner notes (Tibro kommun), kf = Kommunfullmäktige

## Where KF minutes live
- Main listing page (all committees): https://tibro.se/kommun-och-politik/sa-styrs-tibro-kommun/kallelser-och-protokoll/
  ("Kallelser och protokoll", Optimizely CMS). Body text: older protocols only from the
  committee offices ("Vill du få tillgång till tidigare års protokoll, kontakta respektive nämnd").
- KF-specific mirror: https://tibro.se/link/8f07178a559e4dbeb342790aff56e9d9.aspx (from the
  Kommunfullmäktige page) - same document set, KF only.
- The whole tree is server-rendered in the DOM (collapsed accordions). Expand the top
  accordion (button "Kallelser och protokoll") then each committee; all <a href*="globalassets">
  are present even when collapsed - extract with browser evaluate, no XHR needed.
  Cookie banner must be accepted first ("Godkänn alla kakor") or clicks are intercepted.

## Document URL pattern (live)
https://tibro.se/globalassets/b.-dokumentkatalog-for-tibro.se/01.-kommun-och-politik/
kallelser-och-protokoll/kommunfullmaktige/protokoll-YYYY/protokoll-kf-<DATE>*.pdf
- 2024-2026 files: protokoll-kf-2024-02-26.signerad.pub-3.pdf (ISO date in name)
- 2022-2023 files (now DELETED from live server, 404): protokoll-kf-220131docx.pdf /
  protokoll-kf-230130.signerad.pub-1.pdf (YYMMDD in name)

## Live availability (checked 2026-08-20)
- Live listing + search only expose KF Protokoll 2024, 2025, 2026. NO 2022/2023 KF folder.
- Every 2022/2023 KF file URL now returns 404 on tibro.se (they were still live in
  June 2025 per Wayback captures 20250619/20250622, removed since).
- Site search (server-rendered): https://tibro.se/sok//Index?q=<query>&filter=Documents&page=N
  indexes only currently-listed PDFs; 2022/2023 KF not findable there.
- Other committees (BUN, KFN, SHBN, SN, KTN, VN, KS) still list older years (2021+) - not KF.

## KF minutes recorded (36 total, 2022-01-01..2026-08-20)
2022 (9, via Wayback): 01-31, 03-28, 04-25, 05-23, 06-13, 09-26, 10-31, 11-28, 12-19
2023 (8, via Wayback): 01-30, 02-27, 03-27, 04-24, 06-12, 09-25, 10-30, 11-27
2024 (7, live): 02-26, 03-25, 04-29, 06-17, 09-30, 10-21, 11-25
2025 (8, live): 02-24, 03-31, 04-28, 06-18, 09-29, 10-20, 11-24, 12-15
2026 (4, live): 02-23, 03-23, 04-27, 06-17
Pattern: ~monthly Mon meetings, no Jan (2024+) and no July/Aug; no Dec meeting in 2023.

## Wayback sources for 2022/2023 KF minutes
- Listing page captures (complete year lists):
  2022 full list: https://web.archive.org/web/20230206213229/https://tibro.se/kommun-och-politik/sa-styrs-tibro-kommun/kallelser-och-protokoll/
  2023 full list: https://web.archive.org/web/20240222073545/https://tibro.se/kommun-och-politik/sa-styrs-tibro-kommun/kallelser-och-protokoll/
  (other useful: 20220517063935, 20220930150105, 20231203000002, 20240522164556)
- PDF captures (use https://web.archive.org/web/<ts>id_/<original-url>):
  2022 PDFs: 220131->20231029034210, 220328->20220409203641, others->20230509131626 (www.tibro.se)
  2023 PDFs: 230130->20250619183432, 230227->20250622184625, 230327->20250619190537,
  230424->20250619192703, 230612->20250622165827, 230925->20250619194730,
  231030->20250619194633, 231127->20250619194220
- Verified several downloaded PDFs: header "Kommunfullmäktige SAMMANTRÄDESPROTOKOLL",
  sammanträdesdatum matches filename date. Confidence 0.9 for wayback copies.

## Excluded (same pages, not KF minutes)
- "Protokoll KF ... omröstningsbilaga ..." (voting-record appendices)
- "Protokoll KF ... protokollsanteckning ...", "bilaga ... skriftlig reservation ..."
- "Kallelse ...", "Handlingar för möte ..." (agenda/notice + meeting documents)
- Other committees' protocols (KS, BUN, KFN, SHBN, SN, VN, KTN)

## Tips for next runs
- Accept cookie banner, expand "Kallelser och protokoll" -> Kommunfullmäktige, evaluate
  all a[href*="globalassets"] to harvest live 2024+; match date from filename.
- For 2022/2023 use the Wayback URLs above (recorded with confidence 0.9). If the
  municipality re-publishes older years, prefer live URLs.
- Wayback CDX is flaky (503/429); use the browser and retry, or use wildcard redirects
  /web/2024id_/<url> to resolve concrete capture timestamps.
