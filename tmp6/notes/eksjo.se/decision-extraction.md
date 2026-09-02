# Eksjö KF decision extraction

- Each protocol page/§ has a "Beslut" section headed "Kommunfullmäktige beslutar att ...".
  Extract every § with a Beslut section, including "anmäls" / "notera informationen"
  items (in this municipality they are recorded as formal decisions, not skippable
  announcements).
- § without a Beslut section (e.g. "Allmänhetens frågestund") is Q&A/discussion - skip.
- Interpellations close with the decision "att anse interpellationen besvarad" - extract.
- Reservations, jäv and "deltar inte i beslutet" notes are printed directly under the
  decision text - keep them in summary/outcome where relevant.
- Formal votes (omröstning) are described inline in the § and also in a "Bilaga"
  voting table at the end of the protocol; voting_method is only recorded when a vote
  actually took place (otherwise propositions/acclamation).
- Valärenden (election of members/suppleants) and avsägelser are formal decisions -
  extract them.
