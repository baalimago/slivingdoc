# Arvika KF decision extraction guidance

- Arvika KF protocols carry a "Beslut" block per paragraph; decision text follows
  "Kommunfullmäktige beslutar/konstaterar ...".
- Skip question-session paragraphs (Allmänhetens frågor, Enkla frågor) whose Beslut
  only says "Frågorna och svaren läggs till handlingarna." — filing-only, not a
  formal decision outcome (same rule as aneby.se for file-only beslut).
- Skip "Information till Kommunfullmäktige" paragraphs whose Beslut only says
  "Kommunfullmäktige tar del av informationen." — information item.
- Keep remiss paragraphs: "Motionen/Medborgarförslaget remitteras till
  Kommunstyrelsen för beredning." is a procedural decision (like aneby.se
  "överlämna till kommunstyrelsen").
- Keep motions/medborgarförslag dispositions (återremiss, anses besvarad,
  konstateranden), val/avsägelser, and address decisions.
- When a counter-yrkande is voted down, the protocol records a
  "Propositionsordning" line ("Ordföranden ställer förslagen mot varandra och
  finner att ...") without vote counts; omit voting_method unless an omröstning
  is explicitly recorded, but do record the reservation when one is noted.
- Attendance lists and yrkanden render Arvikapartiet as (AP)/(ArvP) — canonical
  Arvikapartiet (mapping already present).
