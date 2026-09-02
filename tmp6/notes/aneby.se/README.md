# aneby.se scanner notes (Aneby kommun)

## Where KF (Kommunfullmäktige) minutes live
- Entry: https://aneby.se/sidor/kommun-och-politik/politik-och-beslut/moten-och-protokoll.html
  ("Möten och protokoll", SiteVision CMS). This is THE official protocol archive page:
  one long static HTML list of PDF links grouped by committee heading
  (Allmänna utskottet, Kommunfullmäktige, Kommunrevisionen, Kommunstyrelsen,
  Samhällsbyggnadsnämnden, etc.), newest first, going back years.
- No diarium / no JavaScript listing needed; all protocol links are plain <a href>
  to https://aneby.se/download/<node>/<timestamp>/<filename>.pdf. slim_http finds them
  with required_tokens ["text=Kommunfullmäktige","download"] (note: the word
  "fullmäktige" alone matches every link because the shared nav context contains
  "Protokoll Kommunfullmäktige"; filter on the link text instead).
- Site full-text search https://aneby.se/ovrigt/sok.html?query=... indexes the same PDFs
  (e.g. "Protokoll KF" -> 104 hits) and confirms the page list is complete; it also
  surfaces non-KF items like "KF presidium protokoll" and "Protokoll Valberedning KF" —
  skip those, they are not Kommunfullmäktige meeting minutes.

## URL shape
- KF protocol PDFs: https://aneby.se/download/18.<hexnode>/<unixms>/Protokoll%20KF%20YYYY-MM-DD.pdf
  (older 2022 files named Protokoll%20Kommunfullm%C3%A4ktige%20YYYY-MM-DD.pdf or similar).
- download_document works fine on these (200, application/pdf).

## KF harvest result 2022-01-01..2026-08-19
- 39 meetings, one protocol PDF per meeting date (all verified SAMMANTRÄDESPROTOKOLL,
  "Sammanträde med Kommunfullmäktige"):
  2022: 02-28, 03-28, 05-02, 06-20, 09-26, 10-24, 10-31, 11-28, 12-12 (9)
  2023: 02-27, 03-27, 04-24, 06-19, 06-29, 09-25, 10-30, 11-27, 12-11 (9)
  2024: 01-22, 02-26, 03-25, 04-29, 06-17, 09-23, 10-28, 11-25, 12-09 (9)
  2025: 02-24, 03-31, 04-28, 06-16, 09-29, 10-27, 11-24, 12-15 (8)
  2026: 02-23, 03-30, 04-27, 06-15 (4; next scheduled 09-28, outside range)
- No KF meetings in January, May, July, August (2026 schedule page
  moten-och-protokoll/sammantradesdagar-2026.html confirms the pattern).

## Extraction conventions (Aneby KF)
- Protocol pages use "KF § n" headers with a "Beslut" block; each paragraph's beslut is
  the decision outcome (godkänna, fastställa, anta, bevilja, avslå, bifalla, utse).
- Include procedural paragraphs (§ Upprop, § Val av justerare, § Godkännande av
  föredragningslista) as decisions — they carry explicit "Kommunfullmäktige beslutar".
- Skip pure information-filing paragraphs whose only beslut is "lägga informationen till
  handlingarna" (§ Anmälningsärenden, interpellation svar). But keep one if it adds a
  real action beyond filing (e.g. "…samt anmäla beslutet till kommunrevisionen").
- Medborgarförslag paragraphs decide bifalla/avslå on the proposal plus "ställa sig bakom
  upprättat svar"; record both att-satser in one decision.
- When an att-sats is amended by a yrkande and the chair finds KF decides per the
  ändringsyrkande, record the amended wording as the decision; note the amendment in
  outcome. "Omröstning begärs" without a reported tally -> voting_method notes the vote
  was requested, no result given.
- Attendees listed under "Beslutande"/"Tjänstgörande ersättare"; paragraph ranges next to
  names (e.g. "§§ 71 – 84, 86 – 90") are useful identifiable_tags. Skip "Ej närvarande".

## Dead ends
- Digital anslagstavla (https://aneby.se/arkiv/digital-anslagstavla.html) only holds
  kungörelser (building permits etc.), not protocols; year query params were ignored.
- There is no separate KF page under "Politik och beslut"; "Möten och protokoll" is the
  single source.
