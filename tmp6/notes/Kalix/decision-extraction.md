# Kalix KF decision extraction guidance

- Kalix KF protocols have a "Kommunfullmäktiges beslut" block per §.
- Skip pure information/announcement items: "Information" sections without a
  beslutsblock, "Upprop" without explicit "Kommunfullmäktige beslutar" wording,
  "Meddelanden" (lägga till handlingarna), and "Motioner under beredning"
  (tagit del av redovisningen).
- Skip annual-report/verksamhetsberättelse filings whose beslut only says
  "ta del av ... och lägga den till handlingarna" (e.g. company årsredovisningar
  and some nämnd VB items) - those are noting/filing, not approval.
- Keep verksamhetsberättelse/årsredovisning items whose beslut says
  "godkänna"/"anta" - those are real approvals. Note: several VB paragraphs
  (e.g. Samhällsbyggnadsnämnden, Fritids- och kulturnämnden, Kommunstyrelsen,
  Revision) are printed as "Kommunstyrelsen beslutar" even in the KF protocol -
  quote text as printed.
- Avsägelser, val av ledamöter/ersättare/ombud (including "lämna ärendet till
  nästkommande sammanträde"), interpellationsbeslut ("anse interpellationen
  besvarad", "lämna ärendet till ..."), motionssvar (avslå/bifalla/anses
  besvarad) och motioner som överlämnas för beredning, samt godkännandet av
  tillkännagivandet (§ med "beslutar godkänna tillkännagivandet"), are formal
  decisions - extract.
- Record reservations when printed under the beslut; include in full_text/outcome.
- Most decisions are by proposition/acclamation (beslutsgång "finner att
  kommunfullmäktige beslutar...") - omit voting_method then. BUT a formal
  omröstning with vote counts CAN appear (e.g. 2023-11-27 §195 minoritetsåterremiss:
  20 JA / 15 NEJ). When counts are printed, record voting_method and the result.
- Party codes to expect: S, M, MP, V, L, C, SD, FIK/FIKX (Framtid i Kalix),
  OBER (oberoende).
