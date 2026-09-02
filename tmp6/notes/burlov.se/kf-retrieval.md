# Burlöv (burlov.se) — kf (kommunfullmäktige) retrieval

## 2026-08-22 — first harvest (kf minutes 2022-01-01..2026-08-22)

Site is SiteVision. Public path: Kommun & politik → Möten, handlingar och protokoll →
Kommunfullmäktige.
KF page: https://burlov.se/kommunpolitik/motenhandlingarochprotokoll/kommunfullmaktige.4.6a5bf3a2172841d502a38a18.html
("Här hittar du kallelser och protokoll från kommunfullmäktiges sammanträden.")

### Key discovery: Netpublicator JS module
The KF document list is rendered by Netpublicator (docs.netpublicator.com). The page loads
np-kommunfullmaktige.js which calls the public API. No guessing needed — hit the API directly:

- Reader name: r20758068
- API base: https://docs.netpublicator.com/api/public/
- rootId (KF channel): 6140e39f217d1495938, previousPath[0]: 0a3a8d2ed2f9258
- List meetings of a channel: GET .../read?hash=0a3a8d2ed2f9258-6140e39f217d1495938-{childId}&isr=false
  (childId = year channel, e.g. 2022: d02e9d6d3a835345954, 2023: a26505786e766511613,
   2024: 6fd000c7d84e7856847, 2025: 7601c67eb8309120338; 2026 meetings are listed directly
   under the root KF hash 0a3a8d2ed2f9258-6140e39f217d1495938).
- List items of a meeting: same read endpoint with hash=0a3a8d2ed2f9258-6140e39f217d1495938-{meetingId}
- Download document: GET .../document/{documentId}?hash=0a3a8d2ed2f9258-6140e39f217d1495938-{meetingId}
  (works without cache param; returns application/pdf).

Each meeting items[] contains documents ("KF YYYY-MM-DD - Protokoll" = minutes, "Kallelse förstasida"
= agenda — exclude agenda) and channel items (numbered agenda points). Kallelser are NOT recorded.

### KF meetings found (46, all recorded)
2022: 02-07, 03-21, 04-25, 05-23, 06-20, 09-26, 10-17, 11-07, 11-14, 12-12
2023: 02-06, 03-20, 04-24, 05-22, 06-19, 09-25, 10-23, 11-13, 12-11
2024: 02-05, 03-18, 04-22, 05-20, 06-17, 06-25, 08-19, 09-30, 11-11, 12-09
2025: 01-13, 02-10, 03-17, 04-07, 05-19, 06-16, 09-29, 11-10, 11-24, 12-08
2026: 01-26, 02-09, 02-17, 03-16, 04-13, 05-25, 06-15 (latest as of harvest date)

### Side discovery: legacy archive "Protokoll KS, KF"
Publication root (hash=0a3a8d2ed2f9258, isr=true) also exposes channel 196b2f13e15d1764890
"Protokoll KS, KF" → Kommunfullmäktige (b42122fb42571799507) → year channels 2016..2026.
It mirrors the same 46 KF protocols (different document IDs, same files) PLUS presidium
("KF pres") protocols that are NOT on the public KF page:
KF pres_2023-04-20, KF pres_2023-06-05, KF pres_2023-10-05, KF Pres_2024-04-15,
KF Pres 2025-05-28. Decision: these are presidium meetings (not full KF sammanträden) and are
not listed on the public KF page, so they were NOT recorded for kf. Note for future agents if a
"kf pres" harvest is ever wanted — they are only reachable via the API, not the public menu.

### Verification
Downloaded KF 2026-06-15 - Protokoll PDF (3.1 MB) and extracted text: it is a full
BURLÖVS KOMMUN / Kommunfullmäktige SAMMANTRÄDESPROTOKOLL with § 70–86. Confirms minutes.

### Other notes
- Anslagstavla (bulletin board) page lists recent justerade protocols (all bodies, incl.
  samhällsbyggnadsnämnd AU, socialnämnd AU) — not needed for kf.
- "Möten, handlingar och protokoll" landing page is mostly static text; the real content is the
  per-body Netpublicator module. Other bodies (KS, nämnder) use the same reader r20758068 with
  their own rootId; KS rootId = 074943d6bb361495943.
