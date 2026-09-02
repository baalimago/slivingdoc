# nassjo.se scanner notes (Nässjö kommun), kf = Kommunfullmäktige

## Where KF protocol PDFs live (authoritative)
- The public diarium "EvoInternet" Angular app at https://diariet.nassjo.se/documents
  (linked from nassjo.se "Handlingar, ärenden, kallelser och protokoll i diariet").
- REST API (JSON): GET https://diariet.nassjo.se/api/documents?page=N&pageSize=N&filter=...&orderBy=createDate%20desc
  - filter syntax: attribute:value;attribute:value; e.g.
    filter=unitCode:KS;description:protokoll%20kommunfullm%C3%A4ktige;
    filter=unitCode:KS;description:kommunfullm%C3%A4ktige;documentTypes:PRO;
  - documentTypes filter works per single code (PRKOPI, PRO, PRO2, INKPR NY, PROT NY, PROT...).
  - Also /api/units (unit codes: KS=Kommunstyrelsen etc.), /api/params/document (filter schema).
- PDF download: GET https://diariet.nassjo.se/api/documents/{document-guid}.bin
  (returns application/pdf; discovered via the UI "Hämta fil" click -> download URL).

## KF protocol patterns per year (Kommunfullmäktige = unitCode KS, dept ADA)
- 2022: PRO type, "Protokoll kommunfullmäktige den <date> utan personuppgifter / maskerade
  personuppgifter / (för publicering)" — public version has fileId.
- 2023: PRO type, "... för publicering" / "... utan personuppgifter".
- 2024: PRO type mostly; 2024-12-12 is PRKOPI "Publikt Protokoll...".
- 2025: PRKOPI / PRO2 / INKPR NY "Publikt protokoll...".
- 2026: PRKOPI "Publikt protokoll..."; 2026-02-26 is INKPR NY "Kommunfullmäktige den 26
  februari 2026, utan personuppgifter" (description does NOT contain "protokoll"!).
- Each meeting also has "Arkivbeständigt protokoll ..." (PRO2) usually WITHOUT fileId
  (no public file) — skip. Skip per-§ split files (§ 106, § 123, § 208, § 153, § 120...)
  when a main file exists; for meetings whose protocol is genuinely split into §-range
  PDFs (2023-03-30, 2023-05-25, 2023-09-28, 2023-11-23, 2024-09-26, 2024-10-31) record
  ONE (the main "för publicering" / first part) per date.
- Skip: Kallelse (agenda/notice, KALL NY / KALL KOP), Voteringslista (VO), Inkomna
  handlingar (LIS), presidium beredning protocols, Beslut (BESL NY), Tjänsteskrivelser.

## KF meetings 2022-01-01..2026-08-20 (44 protocols recorded, one per meeting date)
- 2022 (10): 01-27, 02-24, 03-24, 04-28, 05-19, 06-09, 09-29, 10-27, 11-24, 12-08
- 2023 (10): 01-26, 02-23, 03-30, 04-27, 05-25, 06-08, 09-28, 10-26, 11-23, 12-07
- 2024 (10): 01-25, 02-29, 03-21, 04-25, 05-30, 06-13, 09-26, 10-31, 11-28, 12-12
- 2025 (9): 01-30, 03-27, 04-24, 05-22, 06-12, 09-25, 10-30, 11-27, 12-11
  (no Feb 2025 protocol in diarium; searched "februari 2025" — none)
- 2026 (5, to 08-20): 02-26, 03-26, 04-23, 05-21, 06-11 (2026-01-29 cancelled "Inställt";
  next meeting 09-24 outside range)
- All PDFs text-verified (SAMMANTRÄDESPROTOKOLL Kommunfullmäktige + sammanträdesdatum).

## nassjo.se website
- SiteVision CMS (like eksjo.se). KF pages:
  - Kommunfullmäktige: /kommun-och-politik/politik/sa-styrs-kommunen/kommunfullmaktige.html
  - Protokoll page: /kommun-och-politik/politiska-sammantraden/kommunfullmaktiges-sammantraden/protokoll-kommunfullmaktige.html
    (only "Protokoll 2026 / Protokoll 2025" webapp sections currently; 2022-2024 only in diarium)
  - Meeting dates: /kommun-och-politik/politiska-sammantraden.html
- Playwright browser BLOCKED by nassjo.se (403). Use slim_http / download_document.
- The protokoll page webapp links weren't needed; diarium is complete for 2022+.

## Dead ends
- SiteVision webapp file links not visible to slim_http (JS-rendered); but diarium API
  supersedes them. Playwright works on diariet.nassjo.se (separate domain), not nassjo.se.
