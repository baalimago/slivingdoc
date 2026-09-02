# Sigtuna KF decision extraction guidance

- Sigtuna KF protocols (NetPublicator PDF, see sigtuna.se/kf-retrieval.md) print a
  "Beslut" block per § with numbered points; keep all numbered bullets of one § in a
  single decision entry (e.g. §151 Mål och budget has ~30 points incl. per-nämnd
  sections - keep as ONE decision, append Reservationer when printed).
- Skip "Meddelanden" (§ beslut "lägga förteckningen till handlingarna") - filing/noting.
- Interpellationssvar where beslut is "lägga interpellationen och svaret till protokollet"
  + "bordlägga debatten" ARE formal decisions - include.
- "Anmälan av medborgarförslag" with explicit överlämnande to a nämnd = include
  (substantive remittal); "Anmälan av interpellationer/motioner" with "inga ... har
  anmälts" = no decision, skip.
- Valärenden (entledigande + lämna till länsstyrelsen) and nomineringar
  (vigselförrättare) = formal decisions - include.
- §151 Mål och budget records block-wise omröstningar (huvudförslag vs motförslag)
  with ja/nej/avstår counts per punkt, plus avslagsomröstningar on oppositionsförslag.
  Record the counts in voting_method; acklamation otherwise (omit voting_method).
- Politicians: use "Beslutande" roster; justerare named in Underskrifter are KF members
  (include as politicians, role justerare). Övriga närvarande (officials/nämnd chairs)
  skipped.
