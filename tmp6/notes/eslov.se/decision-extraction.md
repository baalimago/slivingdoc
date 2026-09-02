# Eslöv KF decision extraction guidance

- Decisions appear under a "Beslut" heading per §; extract those blocks. Items without a
  "Beslut" heading (e.g. §77-style information items) are info items — skip.
- "Anmälningar för kännedom" (§88-style) is an announcements item — skip even though it
  carries a closing "Redovisningen läggs till handlingarna". By contrast, a formal
  redovisning item (e.g. "Redovisning av ej slutbehandlade motioner") whose Beslut is
  "Redovisningen läggs till handlingarna" IS a recorded decision — include.
- Interpellation and "Enkel fråga" items close with two decisions: "<X> får ställas" and
  "<X> anses besvarad" — include both.
- Motion remittering items ("Motionen remitteras till kommunstyrelsen med begäran om
  yttrande senast ...") are decisions — include.
- Valärenden/avsägelser (entledigande + fyllnadsval) are decisions; keep all bullets of a §
  in one decision entry.
- Votes: Eslöv protocols do not print omröstning results; omit voting_method unless an
  explicit vote result appears.

## Val av justerare (opening §)
- The opening 'Val av justerare' § has no 'Beslut' heading, but it records an explicit election
  outcome ("X och Y utses att jämte ordförande justera protokollet ..."). Treat it as a formal
  decision and include it. The "no Beslut heading = info item" rule targets information items,
  not elections.
