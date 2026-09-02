# Arjeplog KF decision extraction guidance

- Paragraphs are numbered "Kf § N" in the source; keep that prefix style (e.g. "§77") in paragraph_number.
- Decisions appear under a "KOMMUNFULLMÄKTIGES BESLUT" heading; extract each paragraph with a substantive outcome (godkänna, fastställa, bifalla, avslå, återremittera, bevilja, anta, besluta, lämna medgivande, revidera).
- Routine items carry no substantive decision and are skipped: interpellationer/frågor with no content, anmälan av motioner/entlediganden with none, delgivningar and anmälan av beslut from other boards ("läggs till handlingarna"), and redovisning/rapport items.
- Counted omröstningar DO occur in this protocol type (unlike some other chains): they list ja/nej voters by name with party in parentheses. Record as votes (yes/no), with voting_method describing ja/nej meaning, and proposals from the "Förslag till beslut på mötet" section with proposers (including "med biträde av" supporters).
- A minoritetsåterremiss passes when the återremiss gets at least 1/3 of the votes even if it has fewer yes than no; outcome may read "Återremitterad (minoritetsåterremiss)".
- Jäv declarations appear under a "Jäv" heading inside decision paragraphs; keep them in the decision full_text (no separate protocol_notes section in this format).
- "Reservation" sections list members who reserve against a decision; record them in reservations.
- Local party abbreviation FoAr → Folkinitiativet Arjeplog (canonicalized); S/L/V/C are national parties.
- The document text (organization name, place) is authoritative even if a contamination-sweep rejection names another municipality; reject only when the meeting type does not match.

# arjeplog.se scanner notes (Arjeplogs kommun)

## Cross-municipality contamination sweeps
- Contamination sweeps may flag Arjeplog KF protocols as belonging to a
  different target municipality (e.g. "Berg"). This chain's target is Arjeplog:
  genuine Arjeplog protocols carry their own header (ARJEPLOGS KOMMUN) and
  Arjeplog-specific markers (Medborgarhuset, Stiftelsen Arjeploghus,
  Hornavanskolan, Björkbacken, KHF Arjeplog, Öberget 1:10, Norrbotten
  references). When the document shows these, extract with organization_name
  "Arjeplogs kommun"; do not reject on the sweep message alone.
- Party abbreviation FoAr = Folkinitiativet Arjeplog (see canonicalization.md).
