# L4d sources: the Baseten key is kept in a file

### D1. The same way as the Meta key: a file with mode 0600
The stack keeps the Meta Model API key in `$V3RDE_CFG/meta_model_api_key` (default `~/.config/v3rde`) and stops when the mode is not 0600 (`tools/v3rde_stack.sh` on `services`). The Baseten key goes next to it, in `baseten_api_key`, with the same rule, so it is not in the shell history, the repo or the chat.
Environment variables pass to child processes and can show in logs (CWE-526), so the file is the first way.
Source: https://shoulder.dev/threats/cwe/CWE-526

### D2. The key is typed hidden and written atomically
The setup script reads the key with `read -s` (no echo) from the terminal, writes a temporary file under `umask 077` with mode 0600, and moves it into place, so no other user can read it at any time and a stopped run leaves no half file.
Source: https://www.iitk.ac.in/LDP/LDP/Bash-Beginners-Guide/html/sect_08_02.html

### D3. The environment variable stays as a second way
With no key file, `cmd/translate` uses `BASETEN_API_KEY`, the name in Baseten's setup, so a one-time run can still pass the key for one shell.
Source: https://docs.baseten.co/inference/model-apis/overview
