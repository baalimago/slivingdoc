# Kiruna KF decision extraction

- Kiruna Kommunfullmäktige protocols (Ciceron template) use a
  "Kommunfullmäktige beslutar således" block per §. Extract those blocks.
- Skip announcement/information/question items even when they carry a beslut
  block with only a filing/closing wording: "Anmälan av gruppledare" (lägga
  anmälan till handlingarna), "Information om ..." (lägga informationen till
  handlingarna), "Fråga ..." (anse frågan besvarad).
- Extract bordläggning-only val items (e.g. val till Sámi teáhter, bad- och
  tvättstugeföreningar, RKM etc.) - they are formal decisions to postpone; mark
  outcome "Bordlagd".
- Election paragraphs list many seats as "(Bordläggning majoritet)"/"(Bordläggning
  opposition)" - quote them as printed in full_text; the elected names are the
  decision substance.
- When votering is held, record counts in voting_method with which side each
  vote supported (e.g. §122 casting-vote outcome: 22 ja/22 nej, ordförandens
  utslagsröst; §165/§166 dual votes; §178 name vote 26/14).
- Party codes to expect: S, V, SL (Sámelistu), FI, KD, M, C, SJVP, SD.
