package adc

const (
	FeaBASE = "BASE" // Base protocol ( http://adc.sourceforge.net/ADC.html )

	// http://adc.sourceforge.net/ADC-EXT.html

	FeaTIGR = "TIGR" // Tiger hash support
	extONID = "ONID" // Online services identification
	extBZIP = "BZIP" // bzip2 compression of filelist
	extTS   = "TS"   // Timestamps in messages
	extZLIF = "ZLIF" // Compressed communication (Full)
	extZLIG = "ZLIG" // Compressed communication (Get)
	extPING = "PING" // Pinger extension (additional info about hub)
	extSEGA = "SEGA" // Grouping of file extensions in search
	extUCMD = "UCMD" // User commands
	extADCS = "ADCS" // ADC over TLS

	extADC0 = "ADC0" // TODO: links
	extNAT0 = "NAT0"
	extASCH = "ASCH"
	extSUD1 = "SUD1"
	extSUDP = "SUDP"
	extCCPM = "CCPM"

	// Active markers

	extTCP4 = "TCP4"
	extTCP6 = "TCP6"
	extUDP4 = "UDP4"
	extUDP6 = "UDP6"
)

var (
	supExtensionsHub2Cli = ModFeatures{FeaBASE: true, FeaTIGR: true} // TODO: PING, UCMD
	supExtensionsCli2Hub = ModFeatures{FeaBASE: true, FeaTIGR: true}
	supExtensionsCli2Cli = ModFeatures{FeaBASE: true, "BAS0": true, FeaTIGR: true, extZLIG: true, extBZIP: true}
	extFeatures          = ExtFeatures{extADC0, extSEGA}
)
