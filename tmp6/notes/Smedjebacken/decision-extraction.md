# Smedjebacken KF decision extraction guidance

- Standard KF protocol (SAMMANTRÄDESPROTOKOLL, Smedjebackens kommun): each § has a
  "Kommunfullmäktiges beslut" block; extract numbered decision points verbatim.
- Distinguish beslut wording: "Informationen tas emot och läggs till handlingarna"
  (info/filing - skip, e.g. information items, delgivningar) vs "Informationen godkänns"
  (real approval - keep, e.g. ekonomisk uppföljning).
- Avsägelser (godkänns + fyllnadsval/utses) and val are formal decisions - keep.
- Reservations printed under the beslut (e.g. SD-ledamöter) go into full_text/outcome.
- Attendance list: ledamöter with ordförande/vice ordförande, tjänstgörande ersättare
  (note paragraph range, e.g. "§§ 3-14"), and ej tjänstgörande ersättare all listed;
  justerare can be tagged. Officials (sekreterare, kommunchef) are not politicians.
- No counted omröstning in these protocols -> omit voting_method.
- Ceremony/announcement paragraphs without a "Kommunfullmäktiges beslut" block
  (e.g. § "Uppmärksammande av idrottsprestation") are not decisions - skip.
- Remittering of motions and överlämnande of medborgarförslag to nämnd are formal
  decisions - keep.
