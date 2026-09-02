# ockero.se retrieval log (Öckerö kommun), kf = Kommunfullmäktige

## kf — round 1 (2026-08-20): SUCCESS, 36 protocol documents recorded (2022-02-03 .. 2026-06-29)

- ockero.se is SiteVision CMS, but the meeting archive is a NetPublicator
  (Axiell) public reader embedded on the "Protokoll och kallelser" page:
  https://ockero.se/kommun-och-politik/protokoll-och-kallelser
  (root -> Kommun och politik -> Protokoll och kallelser). Body channels on the
  page: Arvodesberedningen, Jävsutskottet, Kommunfullmäktige, Kommunstyrelsen,
  Miljö- och samhällsbyggnadsnämnden, Senior- och funktionshinderrådet,
  Socialnämnden, Utbildnings-, kultur- och fritidsnämnden, Valnämnden,
  Ö-samråd och kommundialog.

## NetPublicator API (JSON, GET) — no Playwright needed beyond discovery
- reader name: r02232432 ; root hash prefix: c555ef98d410318
- channel read: https://docs.netpublicator.com/api/public/r02232432/read?hash=<root>-<channelOrMeetingId>&isr=false
  (works with slim_http; the browser used &jsoncallback=... but it is optional)
- document download (HEAD+GET 200, works with download_document):
  https://docs.netpublicator.com/api/public/r02232432/document/<docId>?hash=<root>-<parentId>&cache=0
  The <docId> is a UUID-ish string; parent = the meeting/channel the doc sits in.
- search: https://docs.netpublicator.com/api/public/r02232432/search/?query=<q>&hash=c555ef98d410318-983e31d64c278064465

## Tree under the KF channel (id 4376d891e72d8064468)
- Root channel (Protokoll och kallelser) id 983e31d64c278064465.
- KF channel lists meetings directly for 2025-04 .. 2026-06 and year subchannels:
  - "Kommunfullmäktiges sammanträden 2025" (50eb65843dff7823133): 2025-01-30, 2025-03-06
  - "Kommunfullmäktiges sammanträden 2024" (eb908d51c5e26446981): 8 meetings
  - "Kommunfullmäktiges sammanträden 2023" (ad4a9eb8798a5240720): 2023 meetings
  - "Protokoll" (11b07955808e3433401) -> "2022" (157f155d32953990329): 2022 protocols
  - "Kallelser" (99642c1237063433400) -> 2022..2019 (kallelser only; cross-checked
    the 2022 kallelse channel 324b8b22e04b5240717 to confirm the 8 meeting dates)
- Meetings: 2022: 8 (02-03, 03-03, 04-28, 06-02, 06-30, 10-20, 11-24, 12-15),
  2023: 8 (02-02, 03-02, 04-27, 06-08, 09-07, 11-09, 11-23, 12-07),
  2024: 8 (01-18, 02-29, 04-25, 06-10, 09-05, 10-03, 11-28, 12-12),
  2025: 7 (01-30, 03-06, 04-24, 06-04, 09-04, 11-27, 12-11),
  2026: 5 (02-04, 04-01, 04-29, 06-04, 06-29). All 36 in range; Sep-Dec 2026 not yet held.

## Document naming per meeting (record ONE per date, skip the rest)
- 2023-2026 meetings: full protocol doc named "Protokoll KF <date>" /
  "Protokoll Kommunfullmäktige <date>" plus a "Kallelse ..." (SKIP) and often an
  "Omedelbar justering"/"OJ"-variant (SKIP partial). Recorded the full protocol.
- 2022-02-03 and 2022-03-03: full protocols as direct docs under Protokoll/2022
  channel ("Protokoll kommunfullmäktiges sammanträde 220203/220303").
- 2022-04-28..2022-12-15: NO single full-protocol PDF. The per-meeting channels
  (e.g. "Protokoll kommunfullmäktiges sammanträde 20220428", id 89c982d336bb4251331)
  hold the minutes split per §: "1s. <date>" (protocol page 1 = SAMMANTRÄDESPROTOKOLL
  cover with date + anslag/bevis) + one PDF per paragraph ("KF NN-22 ...") +
  "Närvarolista". Recorded the "1s. <date>" cover page as the single representative
  minutes doc for those 6 dates (title exactly "1s. YYYY-MM-DD"). OJ variants
  ("1s. <date> - OJ", "KF NN-22 OJ ...") live in separate "Protokoll omedelbar
  justering" subchannels — skip.
- Sample verification: downloaded + text-extracted docs from every year
  (2022-02-03, 2022-03-03, 2022-04-28, 2022-12-15, 2023-02-02, 2023-11-09,
  2024-06-10, 2024-12-12, 2025-04-24, 2025-06-04, 2025-12-11, 2026-02-04,
  2026-06-29): header "ÖCKERÖ KOMMUN SAMMANTRÄDESPROTOKOLL Kommunfullmäktige
  Sammanträdesdatum <date>" matches. Note 2023-02-02 protocol page 1 header
  says 2023-02-01 (typo) but anslag/bevis + dagordning + channel say 2023-02-02;
  recorded 2023-02-02.
- Dates recorded = meeting date; titles as published (link text); source_page =
  https://ockero.se/kommun-och-politik/protokoll-och-kallelser ; confidence 0.95
  (full protocols) / 0.9 ("1s." cover pages).
