# Ekerö KF decision extraction guidance

- Extract paragraphs with an explicit decision outcome (godkänna, anta, bevilja,
  avslå, remittera, "motionen ska anses behandlad", val av justerare, upprop).
- Skip paragraphs whose beslut is only "Kommunfullmäktige noterar informationen"
  (interpellationer för anmälan, anmälningar för kännedom, revisionsberättelse).
- Skip "Överläggningar" paragraphs (debate §) even though headed "Beslut" — they
  only list speakers and ajournering.
- Skip "Kommunfullmäktige konstaterar att inga valärenden inkommit" — a statement,
  not a decision.
- Motionssvar produce explicit outcomes: "motionen ska anses behandlad" or
  "avslå motionen" — both are decisions.
- Annual-report/ansvarsfrihet paragraphs keep all numbered beslut points in full_text.
- Voting: only set voting_method for an explicit counted omröstning; chair-led
  "proposition" without counts → omit voting_method.
