# Tranås KF decision extraction guidance

- Tranås KF protocols (MeetingPlus portal PDFs) have a per-§ decision block
  "Kommunfullmäktige beslutar (enligt kommunstyrelsens/presidiets förslag) att ...".
- Procedural opening paragraphs (Upprop, Val av justerare, Fastställande av
  föredragningslista) carry explicit "beslutar att" wording and are kept as
  decisions.
- Medborgarförslag are typically decided as "överlämna medborgarförslaget till
  <nämnd/kommunstyrelsen>" (presidiets förslag) - keep each as a decision.
- Motions answered in KF: avslå/bifalla/anses besvarad - keep. New motions:
  remittera till nämnd - keep.
- Interpellationer/frågor: "frågan får ställas" + "anse frågan besvarad" - keep.
- Avsägelser och fyllnadsval - keep with all att-clauses.
- Skip pure information/filing: "Information om ..." with "lägga informationen
  till handlingarna" (§164-type) and "Anmälningsärenden" godkännande of
  redovisning (§165-type, announcements).
- Reservations printed under the beslut (S/C/MP/V or SD) are included in
  full_text/outcome.
- No omröstning vote counts seen; decisions by proposition/acclamation - omit
  voting_method.
- Party codes: M, S, SD, KD, L, C, MP, V.
