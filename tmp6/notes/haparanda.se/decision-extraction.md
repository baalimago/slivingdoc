# Haparanda KF decision extraction

- Haparanda KF protocols use a "Kommunfullmäktiges beslut" heading per §. Extract
  paragraphs with an explicit decision outcome (godkänna, anta, fastställa, avslå,
  remittera, överlämna, välja, entlediga).
- Skip question/debate sessions even though they carry a "beslut" heading:
  "Allmänhetens frågestund" and "Enkel fråga"/"Interpellation" items (decision is only
  "får ställas och är besvarad"). This matches the ostersund.se note; the eksjo.se note
  says to extract interpellation closings — Haparanda follows the Östersund pattern
  (same Ciceron platform/template).
- Skip "Delgivningar" (information items) even though it says "tar med godkännande del
  av informationen".
- Do extract: upprop/val av justerare/dagordning (§1), medborgarförslag referrals,
  motionssvar/remitteringar, delårsrapport + revisionsgranskning (godkännande),
  verksamhetsorganisation changes, taxor/avgiftsbeslut, sammanträdesplan,
  valärenden (fyllnadsval/avsägelser/entlediganden), and ajournering for extra
  årsmöten (ansvarsfrihet items).
- When an omröstning is held, record counts in voting_method (e.g. "18 nej mot 12 ja")
  and the winning outcome in outcome; note which side each vote supported.
