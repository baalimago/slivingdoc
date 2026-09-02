# alvesta.se scanner notes

- Kommunfullmäktige protocols (Alvesta kommun) mark each item with a "Beslut"
  heading, but items that merely "noterar informationen" (e.g. Aktuellt från
  revisorerna, Redovisning av SAM, Meddelanden) are information items and must
  be skipped even though they carry a Beslut line. Only Beslut sections with a
  substantive outcome (godkänna, anta, utse, entlediga, fastställa, ändra
  datum, uppdrag till nämnd, motion till KS för beredning) are decisions.
- Procedural decisions (§ Närvaro fastställer närvaron, § val av justerare,
  § godkännande av dagordning) are recorded decisions and were included.
- Re-vet contamination sweeps may flag the persisted organization name as not
  matching some other target municipality (e.g. "Arboga"); the protocol text is
  authoritative for organization_name — this chain's KF protocols are Alvesta
  kommun, so keep Alvesta and re-derive from the document.
- Yrkanden/voteringar: record proposals (with proposers) where the protocol has
  a Yrkanden section; keep vote tallies (e.g. "30 JA mot 18 NEJ", sluten
  omröstning counts) in the outcome text since they lack per-party breakdown
  and do not fit the yes/no/abstain votes schema.
