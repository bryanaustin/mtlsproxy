# mtlsproxy

A proxy for receiving or sending mtls connections. This program is written as a service that reloads configurations without disruption when sending the HUP signal.

### Features:
* Reload configurations on HUP
* Read configurations from a directory (like a mtlsproxy.d)
* Read configurations from environmental variables
* Configuration files in [toml](https://github.com/BurntSushi/toml) format
* mtls can run at the ingress end, egress end or both
* Can run multiple proxies in a single instance

## Configuration via Environmental Variables
mtlsproxy uses multiple named profiles to set up configurations. The syntax for each starts with `MTLSPROXY_PROFILE_`, then has the profile name and a suffix for the specific option of that profile. For example:
```
MTLSPROXY_PROFILE_DATABASE_LISTEN=0.0.0.0:12345
```
This will set the `DATABASE` profile's `listen` address to `0.0.0.0:12345`. See the table below for a complete list of suffixes.

*Note:* Nothing stops you from using lowercase profile names, but I would keep them uppercase for readability.

## Configuration via [Toml](https://github.com/BurntSushi/toml) Files
Profiles
Sections in Toml files indicate the profile, and values indicate the options. Using the same example as above:
```
[database]
Listen = "0.0.0.0:12345"
```
This will set the `database` profile's `listen` address to `0.0.0.0:12345`. See the table below for a complete list of suffixes.

## Options:
| Toml Option | Env Option  | Description |
| ----------- | ----------- | ----------- |
| Listen | _LISTEN | The address that this profile will listen on, syntax uses Go address format: [Documentation](https://pkg.go.dev/net#Listen) |
| Proxy | _PROXY | The address that this profile will use for outbound communication, syntax uses Go address format: [Documentation](https://pkg.go.dev/net#Listen) |
| Protocol | _PROTOCOL | The network protocol expected for ingress and egress. Options are the same as Go's network option: [Documentation](https://pkg.go.dev/net#Listen). Defaults to `tcp` |
| ListenCertPath | _CERT_LISTEN | The filesystem path to the certificate that will be served on inbound communication |
| ListenCertRaw | - | The certificate in PEM format to the certificate that will be served on inbound communication |
| ListenPrivatePath | _PRIVATE_LISTEN | The filesystem path to the private certificate used for inbound communication |
| ListenPrivateRaw | - | The certificate in PEM format to the private certificate used for inbound communication |
| ListenAuthorityPath | _AUTHORITY_LISTEN | The filesystem path to the certificate authority used for validation of inbound communication |
| ListenAuthorityRaw | - | The certificate in PEM format to the certificate authority used for validation of inbound communication |
| SendCertPath | _CERT_SEND | The filesystem path to the certificate that will be used on outbound communication |
| SendCertRaw | - | The certificate in PEM format to the certificate that will be used on outbound communication |
| SendPrivatePath | _PRIVATE_SEND | The filesystem path to the private certificate used for outbound communication |
| SendPrivateRaw | - | The certificate in PEM format to the private certificate used for outbound communication |
| SendAuthorityPath | _AUTHORITY_SEND | The filesystem path to the certificate authority used for validation of outbound communication |
| SendAuthorityRaw | - | The certificate in PEM format to the certificate authority used for validation of outbound communication |

## Toml Example:
```
[secure-to-unsecured]
Listen = ":443"
Proxy = "localhost:80"
ListenCertPath = "public.crt.pem"
ListenPrivatePath = "private.key.pem"
ListenAuthorityPath = "shared.ca.crt.pem"
```
