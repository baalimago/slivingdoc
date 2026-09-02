# Norsjö KF decision extraction

- Norsjö Kommunfullmäktige protocols (this template) use a
  "Kommunfullmäktiges beslut" bullet block per §.
- Extract: elections (protokolljusterare, nämndval, vice ordförande),
  approvals (godkänns/antas/fastställs), motion responses (anses besvarad,
  avslås), tax/avgifts decisions, and annual-report paragraphs whose beslut
  says "godkänns/antas" (incl. ansvarsfrihet) - real approvals.
- Skip: ajournering paragraphs (§ titled "Ajournering" or info items whose
  only beslut is ajournera/återuppta), opening/closing §§, interpellation
  items answered without a beslutsblock (narrative "förklaras besvarad"),
  and pure information items.
- Vote counts are printed in the attendance table for §§ with votering; §23
  and §24 in 2024-03-25 had formal votering with counts (22/4/1 and 22/5).
  Record voting_method only when counts are printed; otherwise acclamation.
- Jäv noted for tjänsteperson/ersättare (e.g. Ina Jeuthe V, Linda Lidén S)
  does not change the decision text.
- Party codes: S, V, KD, L, M, SD.
