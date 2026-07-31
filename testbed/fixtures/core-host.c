__attribute__((import_module("wippy:http/fetch"), import_name("probe")))
int probe(int a, int b);

__attribute__((export_name("run")))
int run(int a, int b) { return probe(a, b); }
