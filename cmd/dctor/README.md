# Tor bridge for DC

This program implements a fully-functional ADC bridge over Tor.

Features:
- Tor is embedded into the bridge - no need to run separately.
- All features like chat, search, file download/upload are supported.

TODOs:
- Only Tor C-C connections are supported at the moment. Mixed connections can be implemented later.

Since Tor addresses are not known by any hubs or clients, the bridge should be running
both on server side and the client side.

## Hub

On the server side, you need any existing ADC hub that is configured to:
- Pass `INF` extension fields (specifically `EA`).
- Allow multiple users from the same IP (`127.0.0.1`, which is bridge IP).

To run the bridge:
```
./dctor hub adc://localhost:411
```

Where `localhost:411` is an address of the hub.

Wait for the following line to appear:
```
Tor address: adc://xxxxxxxxxxxxxxxx.onion
```

This address should be used in the client bridge to connect to the hub over Tor.

## Client

Any existent client can be used, but you will need to run the client-side bridge:
```
./dctor client adc://xxxxxxxxxxxxxxxx.onion --host=localhost:1412
```

Where `adc://xxxxxxxxxxxxxxxx.onion` is the hub address in the Tor network and
`localhost:1413` is the local address of a bridge that client will use to connect to the hub.

Wait for the following line to appear:
```
Listening for clients on localhost:1412
```

Then connect to `adc://localhost:1412` from your client.