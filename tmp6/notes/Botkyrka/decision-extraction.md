# Botkyrka KF decision extraction

- Protocol layout (opengov 360online): header + "Innehållsförteckning" listing
  "Ärende för beslut" (§ per item), then per-§ blocks with "Beslut",
  "Sammanfattning", "Yrkanden", "Propositionsordning".
- Keep all substantive "Beslut" blocks. Procedural items to KEEP: motion
  "överlämnas till kommunstyrelsen för beredning", interpellationer
  "överlämnas till respektive ordförande för besvarande", enkla frågor
  ("Frågan får ställas" + "Frågan är besvarad").
- SKIP information items: "Anmälningsärenden" whose Beslut only says
  "Kommunfullmäktige noterar informationen till protokollet".
- Avsägelser/fyllnadsval (§): keep as ONE decision entry containing avsägelser,
  förrättade val and bordlagda val.
- Ansvarsfrihet items are real decisions (beviljar ansvarsfrihet); include the
  Jäv block is context, not part of the decision text.
- Voting: "Ordföranden konstaterar ... beslutar enligt <förslag>" - no recorded
  omröstning; omit voting_method.
- Some § include "Kommunstyrelsen har beslutat för egen del: ..." - that is KS
  context, not a KF decision; keep only the KF Beslut block.
