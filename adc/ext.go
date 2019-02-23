package adc

var (
	// FeaBASE indicates base ADC protocol support.
	//
	// See: https://adc.dcbase.org/Protocol, http://adc.sourceforge.net/ADC.html
	FeaBASE = Feature{'B', 'A', 'S', 'E'}
	FeaBAS0 = Feature{'B', 'A', 'S', '0'} // TODO: clarify

	// https://adc.dcbase.org/Extensions
	// http://adc.sourceforge.net/ADC-EXT.html

	FeaTIGR = Feature{'T', 'I', 'G', 'R'} // Tiger hash support
	FeaPING = Feature{'P', 'I', 'N', 'G'} // Send additional field in hub's info message
	extONID = Feature{'O', 'N', 'I', 'D'} // Online services identification
	FeaBZIP = Feature{'B', 'Z', 'I', 'P'} // bzip2 compression of filelist, adds virtual files.xml.bz2 file
	FeaTS   = Feature{'T', 'S', '0', '0'} // Unix timestamps in messages
	extZLIF = Feature{'Z', 'L', 'I', 'F'} // Compressed communication (Full)
	extZLIG = Feature{'Z', 'L', 'I', 'G'} // Compressed communication (Get)
	extPING = Feature{'P', 'I', 'N', 'G'} // Pinger extension (additional info about hub)
	FeaSEGA = Feature{'S', 'E', 'G', 'A'} // Grouping of file extensions in search
	FeaUCMD = Feature{'U', 'C', 'M', 'D'} // User commands
	FeaUCM0 = Feature{'U', 'C', 'M', '0'} // User commands
	FeaADCS = Feature{'A', 'D', 'C', 'S'} // ADC over TLS for C-H

	FeaADC0 = Feature{'A', 'D', 'C', '0'} // ADC over TLS for C-C
	extNAT0 = Feature{'N', 'A', 'T', '0'} // NAT traversal for C-C
	extASCH = Feature{'A', 'S', 'C', 'H'}
	extSUD1 = Feature{'S', 'U', 'D', '1'}
	extSUDP = Feature{'S', 'U', 'D', 'P'}
	extCCPM = Feature{'C', 'C', 'P', 'M'}

	// feature markers to indicate active mode

	// FeaTCP4 should be set in user's INF to indicate that the client has an open TCP4 port (is active).
	FeaTCP4 = Feature{'T', 'C', 'P', '4'}
	// FeaTCP6 should be set in user's INF to indicate that the client has an open TCP6 port (is active).
	FeaTCP6 = Feature{'T', 'C', 'P', '6'}

	extUDP4 = Feature{'U', 'D', 'P', '4'}
	extUDP6 = Feature{'U', 'D', 'P', '6'}
)

const (
	FileListBZIP = "files.xml.bz2"
)
