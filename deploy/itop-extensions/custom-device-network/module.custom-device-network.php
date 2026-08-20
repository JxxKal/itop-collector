<?php
//
// iTop module definition file
// Netzkennung (MAC-Adresse, DNS-Name) an Geraeteklassen.
//

SetupWebPage::AddModule(
	__FILE__,
	'custom-device-network/1.0.0',
	array(
		// Identification
		//
		'label'    => 'Device Network Identification',
		'category' => 'business',

		// Setup
		//
		// Mindestversion, NICHT auf die aktuell installierte Version pinnen ->
		// sonst blockiert die Extension beim naechsten iTop-Update.
		// itop-config-mgmt definiert ConnectableCI und VirtualDevice.
		'dependencies' => array(
			'itop-config-mgmt/3.0.0',
		),
		'mandatory' => false,

		// visible=true und BEWUSST kein auto_select - gleiche Begruendung wie bei
		// custom-agent-inventory: extensionsmap.class.inc.php blendet jedes Modul
		// aus der Extension-Auswahl aus, das visible=false ist ODER ein auto_select
		// gesetzt hat. Unter extensions/ heisst das, man sieht beim Setup nicht, ob
		// das Modul ueberhaupt erkannt wurde.
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
