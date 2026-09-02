# nybro.se scanner notes (Nybro kommun, kf = Kommunfullmäktige)

## Site structure / where KF minutes live
- Main site nybro.se = WordPress (theme nybro3, plugins ellibot_forms_2_0). Static pages + some AJAX widgets.
- "Protokoll och kallelser" page (https://nybro.se/kommun-politik/politik/protokoll-kallelser/)
  now points to Troman Publik: https://nybro.tromanpublik.se/ ("I vårt verksamhetssystem Troman ...").
- Troman Publik is the CURRENT public archive for KF protocols. Entry:
  - Organisation page: https://nybro.tromanpublik.se/organisation/61681487-e492-489b-8187-a72164bd00ac
    (KOMMUNFULLMÄKTIGE, mandatperiod 2022-10-15..2026-10-14; organisationstyp 93f63ae7-... = Kommunfullmäktige).
  - The org page lists "Tidigare möten (10 st)" = ONLY the 10 most recent meetings, server-rendered,
    NO pagination (tested ?page=2, ?year=, ?visa=alla, /moten, /api/*, etc. — all ignored or 404).
    Rolling window: as new meetings are added, old ones drop off AND their /mote/<uuid> pages become 404.
  - Meeting page: https://nybro.tromanpublik.se/mote/<uuid> lists documents as links
    https://nybro.tromanpublik.se/fil?id=<file-uuid> with link texts like
    "<date> - Protokoll från kommunfullmäktige.pdf" (also "Kallelse ...", "Närvarolista", "Voteringslista").
- One meeting = one protokoll PDF; skip Kallelse (agenda/notice), Närvarolista, Voteringslista.
- No API / no sitemap / no robots.txt on nybro.tromanpublik.se. /sok only searches persons, not meetings.
- Troman overview (https://nybro.tromanpublik.se/) lists only ONE KOMMUNFULLMÄKTIGE organisation (current
  mandate period) — no older-mandate org entries, so no 2022-2024 KF meetings ever on Troman.

## KF harvest result 2022-01-01..2026-08-20 (9 protocols recorded, 2025-09-15..2026-06-15)
- 2025-09-15 -> fil?id=83006a3d-2990-488d-a305-7b8ad905f0e3 (mote e7e3979c-16a3-4122-9367-49a5086292c2)
- 2025-10-20 -> fil?id=0de26958-f1d0-48ed-b585-3b8db251899a (mote c8ded247-5aca-425d-8091-50943df582b6)
- 2025-11-17 -> fil?id=46e19a2b-afe2-409c-be5e-851fb13d81f9 (mote 9710609c-31b1-48b0-ba0d-f1f3b502b622)
- 2025-12-15 -> fil?id=95d5bf67-a3e7-4e20-968b-851f6684e79e (mote fa42646f-2807-4480-ad11-8549579758ea)
- 2026-02-23 -> fil?id=b8d24065-57dc-4545-9f66-33f27a19139d (mote 4dd666c0-24b5-45bf-8e1b-84a4b0f5069e; title "2026-02-23 - Kommunfullmäktiges protokoll.pdf")
- 2026-03-16 -> fil?id=27cda984-1a4f-4d58-ba9a-4dd0c252ca3c (mote ec8f6323-241a-41c3-a76f-b21e588f91e6)
- 2026-04-13 -> fil?id=1dd758c0-bba4-49ae-89b0-a3ab0e4f1aa3 (mote 4bd4d545-e30a-492c-99e9-caf1a794b514)
- 2026-05-18 -> fil?id=a879b1ca-9d2a-4f10-800d-3c4e65490425 (mote d515b4ee-e19b-45ab-be74-ee1aaba35c71)
- 2026-06-15 -> fil?id=6fb15847-ca00-4be0-97f9-db403f0988d8 (mote b62ce201-4194-4b6a-98bb-032b92c1e259)
- All 9 verified via document_to_text: first page "PROTOKOLL / Kommunfullmäktige / Sammanträdesdatum <date>".
- 2026-01-19 meeting was CANCELLED ("Kommunfullmäktige (inställt)") — no protocol, do not record.
- Meetings Jul/Aug 2026: none (org page's 10 latest end at 2026-06-15); next KF likely Sep 2026 (outside range).

## Re-harvest 2026-08-20 (this run) — confirmation + Wayback deep-dive
- Live org page still lists the same 10 meetings (2025-09-15..2026-06-15 + 2026-01-19 inställt).
  All 9 protocol fil?id= links re-fetched from their /mote pages, downloaded (200 application/pdf),
  and re-verified by text extraction. Recorded again (accepted).
- Wayback CDX (full domain nybro.tromanpublik.se, collapse=urlkey): captures exist ONLY for the org page
  (20260122141636), the homepage (20260122130254), organisationstyp/person/parti pages (20260122),
  static assets (20250303), and exactly ONE fil PDF: 20260527040943 fil?id=a879b1ca (2026-05-18 protocol).
  NO /mote/<uuid> pages and no other fil?id= PDFs were ever captured -> pre-Sep-2025 protocols not in Wayback.
- Org-page snapshot 20260122141636 lists the 10 meetings then visible, incl. 5 now-purged 2025 meetings:
  2025-02-24 (99f8e34d-10b0-4748-8e63-b2c54d1b95d9), 2025-03-17 (63265902-5d90-45a1-8be3-f60662f0c191),
  2025-04-14 (bb0453e2-879c-400e-a67b-83e86bffa8ee), 2025-05-19 (7c8a847b-9bda-4f73-b490-414ee472c5b8),
  2025-06-16 (312440a5-f511-4dad-8fd8-610d381a8bd2). Live /mote/99f8e34d... now 404 (verified).
  Since neither the /mote pages nor their fil PDFs were archived, 2025-02..2025-08 KF protocols are unrecoverable.

## 2022-01 .. 2025-08 KF protocols: NOT publicly retrievable (dead end, details)
- Troman public listing is a rolling 10-meeting window. Wayback capture 2026-01-22 of the org page
  showed meetings back to 2025-02-24 (UUIDs 312440a5..., 7c8a847b..., bb0453e2..., 63265902..., 99f8e34d...)
  but those /mote pages now return 404 on the live site — old meetings are purged.
- Pre-Troman, protocols were served by nybro.se via Politikerportalen through WP admin-ajax.php
  (action=ellibot_nybro_ajax, input=protokoll, polcat=<committee id>; KF=3 in 2019). That AJAX endpoint
  still answers but returns "Det finns inga registrerade sammanträden för vald nämnd" — data cleared.
- Old archive pages on nybro.se (e.g. /kommun-politik/politik/protokoll-kallelser/tidigare-protokoll-kallelser/,
  titled "Tidigare protokoll och kallelser (till och med januari 2025)", WP page id 1561) are now 404/private;
  their content was loaded via the same AJAX widget. Wayback HAS captures of those pages (many, 2021-2026)
  but only the nav shell — the protocol list itself was never archived (verified 20221207072046 and
  20250131125431 captures = navigation only). admin-ajax.php was captured ~200 times but always WITHOUT
  query string -> useless "0" bodies, no action=ellibot_nybro_ajax responses archived.
- WP REST API (nybro.se/wp-json/wp/v2/pages?search=protokoll) shows only the Troman pointer page + anslagstavlan;
  media library holds no KF protocol PDFs.
- Wayback CDX nybro.se protokoll filter: old /politik-kommun/politik/protokoll-och-kallelser/?event=NNNN and
  ?polcat=NN captures are all 2019 (out of range). Nothing for 2022-2024.
- Conclusion: KF minutes before 2025-09-15 are not retrievable from any live/public URL (incl. Wayback).

## Notes / tips
- To re-harvest later: fetch the org page, take the 10 /mote/<uuid> links, open each, take the
  "Protokoll ..." fil?id= link (skip Kallelse/Närvarolista/Voteringslista), download, verify first page.
- Troman fil?id= URLs download fine (200 application/pdf) via download_document.
- nybro.se WP REST API is open (pages/media/search) but holds no protocol archive.
- Anslagstavlan / evenemang pages on nybro.se do not hold KF protocols.
