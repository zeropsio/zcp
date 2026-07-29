import puppeteer from "/Users/macbook/Documents/Zerops-MCP/zcp/tools/welcome-bridge-harness/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js";
const EMAIL=process.env.ZE_EMAIL, PASS=process.env.ZE_PASS, PROJECT="gRLfpBNrSziMKj0VEfk6vw";
const AGENT=process.env.AGENT||"codex";
const sleep=(ms)=>new Promise(r=>setTimeout(r,ms));
const browser=await puppeteer.launch({channel:"chrome",headless:true,args:["--window-size=1600,1000"],defaultViewport:{width:1600,height:1000}});
try{
  const page=await browser.newPage();
  await page.goto("http://localhost:1111/",{waitUntil:"domcontentloaded",timeout:60000}); await sleep(4000);
  if(await page.$('input[type="email"]')){
    await page.type('input[type="email"]',EMAIL,{delay:5}); await page.type('input[type="password"]',PASS,{delay:5});
    await page.evaluate(()=>{const b=[...document.querySelectorAll("button")].find(x=>x.textContent.includes("Login using email")); if(b)b.click();});
    await sleep(6000);
    const t=await page.evaluate(()=>document.body.innerText);
    if(t.includes("Choose your organization")){
      await page.evaluate(()=>{const els=[...document.querySelectorAll('button,a,[role="button"],div,span')].filter(x=>{const s=(x.textContent||"").trim();return s.startsWith("KRLS")&&!s.includes("AWESOME")&&s.length<40&&x.getBoundingClientRect().height>0;}).sort((a,b)=>a.textContent.length-b.textContent.length); if(els[0])els[0].click();});
      await sleep(5000);
    }
  }
  await page.goto(`http://localhost:1111/project/${PROJECT}?zcpOnboard=1`,{waitUntil:"domcontentloaded",timeout:60000});
  await sleep(600);
  await page.screenshot({path:"shot-skeleton.png"});
  for(let i=0;i<90;i++){ if(await page.evaluate(()=>(document.querySelector("z-zcp-onboard-wizard")?.querySelectorAll(".__agent-btn").length||0)>0)) break; await sleep(100); }
  await sleep(400);
  await page.screenshot({path:"shot-picker.png"});
  // hover the 2nd tile
  const box=await page.evaluate(()=>{const b=document.querySelectorAll("z-zcp-onboard-wizard .__agent-btn")[1]; const r=b.getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2};});
  await page.mouse.move(box.x,box.y); await sleep(500);
  await page.screenshot({path:"shot-hover.png"});
  // pick the target agent
  await page.evaluate((a)=>{const b=[...document.querySelectorAll("z-zcp-onboard-wizard .__agent-btn")].find(x=>new RegExp(a,"i").test(x.textContent)); if(b)b.click();},AGENT);
  await sleep(3500);
  await page.screenshot({path:"shot-after-pick.png"});
  const st=await page.evaluate(()=>{
    const w=document.querySelector("z-zcp-onboard-wizard");
    const inner=w?.firstElementChild;
    const pane=document.querySelector(".cdk-overlay-pane");
    const dlgVisible = pane ? (()=>{const r=pane.getBoundingClientRect(); const el=document.elementFromPoint(r.x+r.width/2, r.y+r.height/2); return !!el && pane.contains(el);})() : false;
    return {wizardText:(w?.innerText||"(none)").slice(0,90), wizardZ:inner?getComputedStyle(inner).zIndex:null, dialogPanePresent:!!pane, dialogOnTop:dlgVisible};
  });
  console.log("VISUAL "+JSON.stringify(st,null,2));
} finally { await browser.close().catch(()=>{}); }
