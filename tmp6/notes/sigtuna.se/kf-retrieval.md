# Sigtuna (sigtuna.se) - KF retrieval notes

Target: Sigtuna kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- SiteVision site (www.sigtuna.se). Entry: https://sigtuna.se/kommun-och-politik.html
  -> Handlingar, beslut och rättssäkerhet -> "Handlingar och protokoll":
  https://sigtuna.se/kommun-och-politik/handlingar-beslut-och-rattssakerhet/handlingar-och-protokoll.html
- The page hosts a NetPublicator public reader widget (JS-rendered). API:
  - Root: GET https://docs.netpublicator.com/api/public/r23934199/read?hash=2a0ff1fb64f2217&isr=true
    (list of channels = nämnder; KF channel id 58b374bb0b891034821)
  - Children: hash = parent hash + "-" + child id; NOTE isr=true returns the
    ROOT list again, use isr=false to get children (this bit me).
  - KF tree: 2a0ff1fb64f2217-58b374bb0b891034821 (meetings 2023-02-02..2026-06-16
    directly; plus year channels "2022 handlingar och protokoll"
    = 916d8dc3bdfa5157414 and older years 2009..2021).
  - Meeting: ...-<meetingId> -> items = documents ("Kallelse ...",
    "Kompletterande kallelse ...", "Protokoll kommunfullmäktige <date>" /
    "Protokoll KF <date>", "Närvaroförteckning ...", "Röstningsbilaga ...",
    "MP Yrkande ...") + channels (the § agenda items).
  - Download: https://docs.netpublicator.com/api/public/r23934199/document/{docId}?hash={path}
    returns the PDF (application/pdf). docId is a UUID-ish string.
- Anslagstavla (https://sigtuna.se/kommun-och-politik/handlingar-beslut-och-rattssakerhet/anslagstavla.html)
  is a NetPublicator bulletin board (static.netpublicator.com/js/bulletinboard/...) showing
  only recent postings - no historical KF protocols there.
- Site search (https://sigtuna.se/sok-pa-webbplatsen.html?query=...) does not index
  NetPublicator documents (search for "protokoll kommunfullmaktige 2024-12-17" -> no hits).

## KF minutes recorded (37, dates below), conf 0.95
2022: 03-31, 04-28, 05-19, 06-16, 10-20, 11-30, 12-15 (7)
2023: 02-02, 03-30, 04-27, 06-15, 08-24, 09-21, 10-26, 11-30, 12-14 (9)
2024: 02-01, 03-21, 04-25, 05-23, 06-18, 09-26, 10-24, 11-28 (8; 12-17 has NO protocol, see below)
2025: 01-30, 03-27, 04-24, 06-17, 09-25, 10-23, 11-27, 12-16 (8)
2026: 01-29, 03-26, 04-23, 05-21, 06-16 (5)
Recorded the ONE combined "Protokoll ..." per meeting date. Verified content of
2026-06-16, 2025-03-27, 2023-11-30, 2022-03-31 in full; 2024-11-28 / 2022-10-20
downloaded as valid PDFs (uniform pattern).

## Notes / gotchas
- 2024-12-17 meeting exists in the reader (agenda + Närvaroförteckning + Kallelse)
  but has NO "Protokoll" document attached - protocol never published in the
  public archive. Nothing recorded for it. If it later appears, hash path =
  2a0ff1fb64f2217-58b374bb0b891034821-03966b93d6dd7695687.
- 2022 had only 7 KF meetings (no Jan/Feb, no Sept - election year 2022-09-11).
- Some meetings publish BOTH the main protocol AND an "omedelbart justerat"
  partial protocol (§-range, e.g. 2025-03-27 §29, 2024-06-18 §95). Record ONLY
  the main "Protokoll ..." per date (one per meeting).
- 2022 protocols titled "Protokoll KF <date>"; 2023+ mostly "Protokoll
  kommunfullmäktige <date>" (2023-11-30 = "Protokoll KF 2023-11-30").
- Meeting date = folder name = date in PDF "Sammanträdesdatum" (verified).

## Reusable API pattern for other nämnder on sigtuna.se
Same reader r23934199; other nämnd channels are in the root read. Per-nämnd
year channels may exist (e.g. KF 2022 channel). Use slim_http with
isr=false; JSON bodies are one line each, readable.
