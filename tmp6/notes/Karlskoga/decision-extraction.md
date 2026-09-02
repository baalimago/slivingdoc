# Karlskoga KF decision extraction guidance

- Decisions appear under a "Kommunfullmäktiges beslut" heading per §; keep all
  bullets of a § in one decision entry, paragraph_number "§NN".
- Skip formally recorded non-substantive items: beslut "Informationen läggs/
  lades till handlingarna" (information items), "Ärendet frångicks" (no
  questions/motions received), and interpellation debate closings (debate
  items). These carry explicit beslut text but are info/announcement/debate.
- Do include: entledigande/valärenden (election decisions, outcome "Val
  genomfört") and adoptions of policies/reglementen/riktlinjer (outcome
  "Antagen").
- Voting method: when the text says the ordförande ställde förslaget under
  proposition och fann att kommunfullmäktige biföll, set voting_method
  "Proposition"; otherwise omit.
- Politicians: "Beslutande" roster plus "Tjänstgörande ersättare" (role
  "Tjänstgörande ersättare"); "Övriga deltagare" are not decision-makers,
  skip them.

## Garbled PDF text (seen on the 2025-12-09 KF protocol)

- karlskoga.se KF PDFs can extract as mojibake (broken font ToUnicode): the
  same plaintext letter may surface as several different glyphs, and distinct
  letters may share one glyph. The mapping is NOT uniform across the document;
  each font subset (per section/page) can have its own encoding. Decode per
  subset using anchor words (e.g., in one body font the token
  "NTVVZDPZEEVINHFGM" = "kommunfullmäktige").
- Section-header digit tokens (e.g., "010.211304", "121342222.") are NOT
  diarienummer: every one of them decodes to the meeting date (2025-12-09) —
  each digit has multiple glyph aliases. Actual § numbers are not recoverable
  from garbled text; fall back to document-order numbering.
- Structural markers that survive the garble: the line
  "123324...<var>...7694895" = "Kommunstyrelsens förslag till beslut" heading,
  and a line "X 33<var>37 5<var>" = the "Kommunfullmäktiges beslut" block.
  Sections lacking the latter are information items (skip them).
- When a section is undecodable, submit its visible text verbatim with empty
  outcome rather than guessing content.
