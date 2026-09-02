# Hudiksvall KF decision extraction guidance

- KF protocols use "Kommunfullmäktige beslutar att ..." blocks per paragraph.
- Keep paragraphs with substantive outcomes: upplåningsramar/borgen, taxor,
  riktlinjer, reglementen, detaljplaner, val av ledamöter/ersättare/revisorer,
  entlediganden (bifall + hemställan om ny röstsammanräkning), motionssvar
  ("anse motionen besvarad" / "avslå motionen"), and "godkänna redovisningen"
  (meddelanden om motioner, delegationsbeslut).
- Skip information-only items: "att tacka för informationen" (befolknings-
  redovisning), "att meddelandet läggs till handlingarna", and interpellation
  paragraphs ("interpellationen får framställas", "anse interpellationsdebatten
  avslutad") — debate/procedural, not substantive decisions.
- Voting: set voting_method only when an explicit omröstning is recorded
  (e.g. Quickchannel with ja/nej counts); chair-led proposition without counts
  → omit.
