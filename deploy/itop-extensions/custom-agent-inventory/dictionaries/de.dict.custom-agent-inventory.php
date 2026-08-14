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
));
