<?php
//
// iTop module definition file
// Traegerfelder fuer den itop-agent-Sync (agent_guid, agent_last_seen) an FunctionalCI.
//

SetupWebPage::AddModule(
	__FILE__,
	'custom-agent-inventory/1.0.0',
	array(
		// Identification
		//
		'label'    => 'Agent Inventory Fields',
		'category' => 'business',

		// Setup
		//
		// Mindestversion, NICHT auf die aktuell installierte Version pinnen ->
		// sonst blockiert die Extension beim naechsten iTop-Update.
		// itop-config-mgmt definiert FunctionalCI, an der die Felder haengen.
		'dependencies' => array(
			'itop-config-mgmt/3.0.0',
		),
		'mandatory' => false,

		// visible=true und BEWUSST kein auto_select - gleiche Begruendung wie bei
		// custom-service-software: extensionsmap.class.inc.php blendet jedes Modul
		// aus der Extension-Auswahl aus, das visible=false ist ODER ein auto_select
		// gesetzt hat. Unter extensions/ heisst das, man sieht beim Setup nicht, ob
		// das Modul ueberhaupt erkannt wurde. Sichtbarkeit ist hier mehr wert als
		// Bequemlichkeit.
		'visible' => true,

		// Components
		//
		'datamodel'   => array(),
		'webservice'  => array(),
		'data.struct' => array(),
		'data.sample' => array(),

		// Documentation
		//
		'doc.manual_setup'     => '',
		'doc.more_information' => '',

		// Default settings
		//
		'settings' => array(),
	)
);
