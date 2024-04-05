## livenotes

### Deploy

The service can be self-hosted or deployed on fly.io.

```bash
flyctl launch

# Create a volume for storage
fly volumes create storage --region bom --size 1
```

### Vim integration

Setup sync between local notes and web service.

```vim
" Automatically sync notes when saving a file in the notes directory
autocmd BufWritePost * call SyncNotes()

function! SyncNotes()
  let notes_directory = expand('%:p:h')
  let config_file = notes_directory . '/.notes_config'

  if filereadable(config_file)
    let username = system('grep "^username=" ' . config_file . ' | cut -d "=" -f 2 | tr -d "\n"')
    let password = system('grep "^password=" ' . config_file . ' | cut -d "=" -f 2 | tr -d "\n"')
    let server = system('grep "^server=" ' . config_file . ' | cut -d "=" -f 2 | tr -d "\n"')
    let filename = expand('%:t')

    let output = system('livenotes_client "' . filename . '" "' . username . '" "' . password . '"' . ' "' . server . '"')
    echo output
  endif
endfunction
```
