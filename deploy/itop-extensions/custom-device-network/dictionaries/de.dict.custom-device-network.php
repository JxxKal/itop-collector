<?php
/**
 * Deutsche Bezeichnungen fuer custom-device-network.
 * Ohne diese Datei zeigt iTop die rohen Attributcodes an.
 *
 * Die Eintraege stehen je Klasse, an der das Feld definiert ist. Erbende
 * Klassen uebernehmen sie automatisch.
 */

Dict::Add('DE DE', 'German', 'Deutsch', array(
	'Class:ConnectableCI/Attribute:dns_name'     => 'DNS-Name',
	'Class:ConnectableCI/Attribute:dns_name+'    => 'Voll qualifizierter oder kurzer DNS-Name des Geraets.',

	'Class:DatacenterDevice/Attribute:macaddress'  => 'MAC-Adresse',
	'Class:DatacenterDevice/Attribute:macaddress+' => 'MAC-Adresse der fuehrenden Netzwerkschnittstelle.',

	'Class:VirtualDevice/Attribute:macaddress'  => 'MAC-Adresse',
	'Class:VirtualDevice/Attribute:macaddress+' => 'MAC-Adresse der fuehrenden Netzwerkschnittstelle.',
	'Class:VirtualDevice/Attribute:dns_name'    => 'DNS-Name',
	'Class:VirtualDevice/Attribute:dns_name+'   => 'Voll qualifizierter oder kurzer DNS-Name des Geraets.',
));
