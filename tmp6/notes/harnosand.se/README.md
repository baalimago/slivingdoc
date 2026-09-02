# harnosand.se scanner notes

- Same pattern as bollnas.se: the scoped domain (harnosand.se) may not match
  the municipality in the document. In one run the protocol was from
  Hudiksvalls kommun (Kommunfullmäktige 2026-06-15) while the target directory
  was harnosand.se. Trust the document text for organization_name/location.
- In Swedish KF protocols, paragraphs with a "Kommunfullmäktige beslutar"
  header may still be pure information/announcement items. Skip them even
  though they carry decision wording, e.g. "att tacka för informationen"
  (information/report item), "att godkänna redovisningen av ..." (delegations-
  beslut / väckta motioner), "att meddelandet läggs till handlingarna"
  (meddelanden). Extract only substantive decisions (budget, regulations,
  entlediganden, interpellationsframställan, ansvarsfrihet etc.).
