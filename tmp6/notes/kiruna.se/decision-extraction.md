# kiruna.se KF decision extraction guidance

- Each paragraph has a "Kommunfullmäktige beslutar att ..." block; extract every
  paragraph that carries an explicit outcome (godkänna, anta, välja, bordlägga,
  avslå, återremittera, tilläggsbudgetera, godkänna dagordning).
- Fyllnadsval paragraphs: keep both "välja X" and "bordlägga val" outcomes; mixed
  paragraphs (one val + one bordläggning) are one decision with all att-clauses.
- Skip question items ("Fråga till ..." where beslut is only "frågan är besvarad"),
  delgivningar ("tagit del av informationen") and information items — Q&A and
  information, not substantive decisions.
- Voting: set voting_method only when an explicit counted omröstning is recorded
  (e.g. §65-type votering with ja/nej/avstår counts); chair-led propositions
  without counts → omit voting_method.
- full_text = the beslutar att-clauses as written; keep numbered/bulleted points.
