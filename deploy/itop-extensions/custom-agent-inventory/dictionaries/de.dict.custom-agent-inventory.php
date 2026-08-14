<?php
/**
 * Deutsche Bezeichnungen fuer custom-agent-inventory.
 * Ohne diese Datei zeigt iTop die rohen Attributcodes an.
 */

Dict::Add('DE DE', 'German', 'Deutsch', array(
	'Class:FunctionalCI/Attribute:agent_guid'       => 'Agent-GUID',
	'Class:FunctionalCI/Attribute:agent_guid+'      => 'Vom itop-agent erzeugte Kennung. Primaerer Abgleichschluessel; ueberlebt Umbenennung, nicht Reimaging.',
	'Class:FunctionalCI/Attribute:agent_last_seen'  => 'Letzte Agent-Meldung',
	'Class:FunctionalCI/Attribute:agent_last_seen+' => 'Zeitpunkt, zu dem der itop-agent zuletzt Daten fuer dieses CI geliefert hat.',
	'Class:Software/Attribute:agent_match_patterns' => 'Zuordnungsmuster (Agent)',
	'Class:Software/Attribute:agent_match_patterns+' => 'Ein Muster je Zeile. Normaler Text trifft, wenn er im gemeldeten Programmnamen vorkommt (Gross-/Kleinschreibung egal). Ein vorangestelltes ! schliesst aus. Ein von Schraegstrichen umschlossenes Muster wird als regulaerer Ausdruck gelesen. Leer heisst: dieser Eintrag nimmt am Abgleich nicht teil.',
));
