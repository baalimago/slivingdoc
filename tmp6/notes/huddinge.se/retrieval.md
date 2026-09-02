# huddinge.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-20): SUCCESS, 41 protocols recorded (2022-02-14 .. 2026-06-15)

- Entry from huddinge.se: "Organisation och styrning" -> "Så styrs Huddinge" ->
  "Möten, kallelser och protokoll"
  (https://huddinge.se/organisation-och-styrning/sa-styrs-huddinge/moten-kallelser-och-protokoll)
  -> Kommunfullmäktige (KF) page
  (https://huddinge.se/organisation-och-styrning/ansvar-och-organisation/kommunens-organisation/politisk-ledning/kommunfullmaktige)
  -> "Möten och handlingar 2019 och framåt" = **https://sammantraden.huddinge.se** (MeetingPlus by Formpipe, sv locale).
- Committee listing: https://sammantraden.huddinge.se/committees/kommunfullmaktige
  has two tabs: "Kommande" (upcoming) and "Tidigare" (past, grouped by year 2026..2019).
  slim_http shows the full list (no pagination); Playwright tab click not needed for the data.
- Meeting pages: https://sammantraden.huddinge.se/committees/kommunfullmaktige/kommunfullmaktige-<id>
  with tabs Kallelse (agenda, skip) / Protokoll (minutes). The Protokoll tab has an "Öppna protokoll"
  link: /committees/kommunfullmaktige/kommunfullmaktige-<id>/protocol/<slug>?downloadMode=open
  -> download with **?downloadMode=download** (GET 200, application/pdf; download_document works fine).
- Meeting ids (slug suffixes) are opaque, not date-derivable; find each protocol link by fetching the
  meeting page and grepping the "Öppna protokoll" href (slim_http with required_tokens ["Öppna protokoll"] works).
- One meeting = one full protocol. When several protocol docs exist per meeting, pick the full one
  ("... med bilagor.pdf" / "... (Med bilagor).pdf" / "... (inkl. bilagor).pdf") and skip:
  "Omedelbar justering" partial docs, "§ N" partial docs (e.g. 2026-06-15 §21 omedelbar justering,
  2025-09-01 §14, 2025-10-13 §12, 2025-12-08 §22-23, 2024-04-22 §16), and all Kallelse docs.
- Cancelled meetings (listed "inställt", e.g. 2022-08-29, 2022-12-15, 2023-11-08, 2024-06-03,
  2024-11-14, 2025-11-12) have "Öppna protokoll" = javascript:void(0) and "Ingen agenda har publicerats";
  no protocol exists - record nothing.
- Recorded 41 (dates as meeting/sammanträdesdatum): 2022: 02-14, 03-21, 04-25, 05-16, 06-13, 08-22,
  10-17, 11-07, 12-12(budget); 2023: 02-13, 03-20, 04-24, 05-15, 06-12, 09-04, 10-09, 11-06(budget),
  12-11; 2024: 02-12, 03-18, 04-22, 05-27, 06-17, 09-02, 10-14, 11-11(budget), 12-09; 2025: 02-17,
  03-24, 04-28, 05-26, 06-16, 09-01, 10-13, 11-10(budget), 12-08; 2026: 02-02, 03-23, 04-27, 05-25,
  06-15. (Budget meetings have their own ids like kommunfullmaktige-budget-98710 and ARE KF protocols.)
- 2026 upcoming (08-31, 10-19) are after range end and have no protocols yet. No 2022-01 meeting exists.
- Confidence 0.95 (0.9 for 2023-10-09 whose slug is the generic "kf-protokoll-med-bilagorpdf" but the
  PDF is the full 2023-10-09 protocol).
- Quirk: 2024-09-02 meeting page URL slug is "kommunfullmaktige-2024-09-02" but its protocol link
  points to "kommunfullmaktige-21866/protocol/protokoll-kf-2024-09-02-med-bilagorpdf" - use the link
  as found (it downloads fine). Also 2022-08-22 protocol slug has no "-med-bilagor" suffix.

## Protocol PDF layout
- 2022-2023 protocols: header "KOMMUNFULLMÄKTIGE SAMMANTRÄDESPROTOKOLL", "Kommunfullmäktiges beslut"
  paragraphs, often "Sammanfattning" + "Överläggning" + "Propositioner" + "Reservationer".
- 2024+ protocols: "SAMMANTRÄDESPROTOKOLL" with Diarienummer, Ärendelista, "Kommunfullmäktiges beslut",
  "Yttranden under sammanträdet", "Yrkanden", "Beslutsgång", "Reservationer", "Särskilt yttrande",
  "Beslutsunderlag". Substantive decisions are the "Kommunfullmäktiges beslut" blocks; skip pure
  information items ("noterar ... till protokollet" for statistikrapporter/delgivningar/revision info,
  "Inga frågor/interpellationer") but DO record procedural items (upprop, justerare, anmälan av
  ledamöter/ersättare, interpellation/fråga "får ställas"/"anses besvarad", motion "får väckas och
  överlämnas till kommunstyrelsen för beredning").
