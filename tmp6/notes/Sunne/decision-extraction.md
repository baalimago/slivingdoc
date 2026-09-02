# Sunne KF decision extraction guidance

- Sunne KF protocols (2022 PDFs, Frykensalen) number paragraphs §N and record
  outcomes under a "Kommunfullmäktiges beslut" block. Extract those blocks.
- Information items (headed "Information om ...", e.g. "Information om hot mot
  demokratin i Värmland, årsrapport 2021") have no "Kommunfullmäktiges beslut"
  block — skip them entirely.
- Keep: motion decisions (avslag / anses besvarad), policy approvals
  (multi-point decisions keep all numbered points), medborgarförslag
  remittering, avsägelser (entledigande + begäran om ny ledamot), fyllnadsval.
- When a counted omröstning table is present (§39-style), put the vote summary
  (ja/nej counts and which förslag each side supports) in voting_method.
- "Yrkanden", "Beslutsgång" and "Reservation" sections are not part of the
  decision text; skip them.
- Attendance list (Beslutande) gives party + ledamot/ersättare role;
  "digitalt" marks remote participation and may go in identifiable_tags.
  "Ersättare saknas" lines name no person — do not emit as politicians.
